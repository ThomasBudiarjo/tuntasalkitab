package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func newTestSigner(t *testing.T) *Signer {
	t.Helper()
	s, err := NewSigner("a-secret-long-enough-for-testing")
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return s
}

func TestSignerRejectsShortSecrets(t *testing.T) {
	if _, err := NewSigner(""); err == nil {
		t.Error("empty secret was accepted")
	}
	if _, err := NewSigner("short"); err == nil {
		t.Error("short secret was accepted")
	}
}

func TestAccessTokenRoundTrip(t *testing.T) {
	signer := newTestSigner(t)
	now := time.Unix(1_700_000_000, 0)

	token, expiry := signer.IssueAccess(42, now)
	if !expiry.Equal(now.Add(AccessTokenTTL)) {
		t.Errorf("expiry = %v, want %v", expiry, now.Add(AccessTokenTTL))
	}

	userID, err := signer.VerifyAccess(token, now)
	if err != nil {
		t.Fatalf("VerifyAccess: %v", err)
	}
	if userID != 42 {
		t.Errorf("userID = %d, want 42", userID)
	}
}

func TestAccessTokenExpires(t *testing.T) {
	signer := newTestSigner(t)
	now := time.Unix(1_700_000_000, 0)
	token, _ := signer.IssueAccess(7, now)

	if _, err := signer.VerifyAccess(token, now.Add(AccessTokenTTL-time.Second)); err != nil {
		t.Errorf("token rejected just before expiry: %v", err)
	}
	if _, err := signer.VerifyAccess(token, now.Add(AccessTokenTTL)); !errors.Is(err, ErrTokenExpired) {
		t.Errorf("err at expiry = %v, want ErrTokenExpired", err)
	}
}

// The whole point of signing: a payload cannot be edited to claim another user.
func TestAccessTokenRejectsTampering(t *testing.T) {
	signer := newTestSigner(t)
	now := time.Unix(1_700_000_000, 0)
	token, _ := signer.IssueAccess(1, now)

	payload, signature, _ := strings.Cut(token, ".")

	// A payload for a different user, carrying the original signature.
	forged, _ := signer.IssueAccess(999, now)
	forgedPayload, _, _ := strings.Cut(forged, ".")
	if _, err := signer.VerifyAccess(forgedPayload+"."+signature, now); !errors.Is(err, ErrBadSignature) {
		t.Error("a swapped payload was accepted")
	}

	// A signature from elsewhere.
	if _, err := signer.VerifyAccess(payload+".not-a-signature", now); !errors.Is(err, ErrBadSignature) {
		t.Error("a bogus signature was accepted")
	}

	// A token signed with a different secret.
	other, err := NewSigner("a-completely-different-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	otherToken, _ := other.IssueAccess(1, now)
	if _, err := signer.VerifyAccess(otherToken, now); !errors.Is(err, ErrBadSignature) {
		t.Error("a token from another secret was accepted")
	}
}

func TestAccessTokenRejectsMalformed(t *testing.T) {
	signer := newTestSigner(t)
	now := time.Unix(1_700_000_000, 0)

	for _, token := range []string{"", "no-dot", ".", "a.b"} {
		if _, err := signer.VerifyAccess(token, now); err == nil {
			t.Errorf("malformed token %q was accepted", token)
		}
	}
}

func TestRefreshTokensAreUniqueAndHashed(t *testing.T) {
	first, firstHash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}
	second, secondHash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}

	if first == second {
		t.Error("two refresh tokens came out identical")
	}
	if firstHash == secondHash {
		t.Error("two refresh tokens hashed the same")
	}
	if strings.Contains(firstHash, first) {
		t.Error("the stored hash contains the token itself")
	}
	if got := HashRefreshToken(first); got != firstHash {
		t.Errorf("HashRefreshToken is not stable: %q vs %q", got, firstHash)
	}
	if len(first) < 40 {
		t.Errorf("refresh token is only %d chars; too little entropy", len(first))
	}
}
