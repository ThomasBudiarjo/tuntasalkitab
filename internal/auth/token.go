// Package auth issues and verifies the tokens the Android client uses.
//
// The website authenticates with a session cookie, which a mobile client cannot
// carry across the Google sign-in flow. The app instead presents a Google ID
// token once and gets a pair of tokens back:
//
//   - an access token: short-lived, stateless, signed here. Verifying it costs
//     one HMAC and no database round trip.
//   - a refresh token: long-lived, random, stored hashed. It is revocable,
//     which the access token is not.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// AccessTokenTTL is deliberately short. The access token cannot be revoked,
	// so its lifetime is the window in which a leaked one still works.
	AccessTokenTTL = time.Hour

	// RefreshTokenTTL matches the website's session cookie, so a reader who
	// signs in on the phone stays signed in for as long as they would on the
	// web.
	RefreshTokenTTL = 365 * 24 * time.Hour

	tokenVersion = "v1"
)

var (
	ErrMalformedToken = errors.New("auth: malformed token")
	ErrBadSignature   = errors.New("auth: bad signature")
	ErrTokenExpired   = errors.New("auth: token expired")
)

// Signer mints and checks access tokens.
type Signer struct {
	secret []byte
}

func NewSigner(secret string) (*Signer, error) {
	if len(secret) < 16 {
		return nil, fmt.Errorf("auth: signing secret must be at least 16 bytes, got %d", len(secret))
	}
	return &Signer{secret: []byte(secret)}, nil
}

// IssueAccess returns a token asserting userID, valid until now+[AccessTokenTTL].
//
// The format is payload.signature, where payload is "v1:<userID>:<expiryUnix>".
// This is a JWT in spirit without the dependency; nothing here needs algorithm
// negotiation, and not having it removes the "alg: none" class of mistake.
func (s *Signer) IssueAccess(userID int64, now time.Time) (string, time.Time) {
	expiry := now.Add(AccessTokenTTL)
	payload := fmt.Sprintf("%s:%d:%d", tokenVersion, userID, expiry.Unix())
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + "." + s.sign(encoded), expiry
}

// VerifyAccess returns the user the token asserts, or an error.
func (s *Signer) VerifyAccess(token string, now time.Time) (int64, error) {
	encoded, signature, ok := strings.Cut(token, ".")
	if !ok {
		return 0, ErrMalformedToken
	}

	// Constant time: a timing oracle here would let an attacker discover a
	// valid signature byte by byte.
	if subtle.ConstantTimeCompare([]byte(signature), []byte(s.sign(encoded))) != 1 {
		return 0, ErrBadSignature
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return 0, ErrMalformedToken
	}

	parts := strings.Split(string(payload), ":")
	if len(parts) != 3 || parts[0] != tokenVersion {
		return 0, ErrMalformedToken
	}

	userID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, ErrMalformedToken
	}
	expiry, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, ErrMalformedToken
	}
	if now.Unix() >= expiry {
		return 0, ErrTokenExpired
	}

	return userID, nil
}

func (s *Signer) sign(encodedPayload string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(encodedPayload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// NewRefreshToken returns a fresh opaque token and the hash to store for it.
//
// Only the hash is persisted, so a leaked database does not hand out working
// sessions. SHA-256 is enough here — unlike a password, the token is 256 bits
// of entropy, so there is nothing to brute force and no need for a slow KDF.
func NewRefreshToken() (token string, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("auth: generate refresh token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashRefreshToken(token), nil
}

// HashRefreshToken maps a refresh token to its stored form.
func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
