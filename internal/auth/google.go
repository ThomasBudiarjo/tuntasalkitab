package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/api/idtoken"
)

var ErrGoogleNotConfigured = errors.New("auth: GOOGLE_CLIENT_ID is not set")

// GoogleIdentity is what a verified Google ID token tells us about a reader.
type GoogleIdentity struct {
	Subject string // the stable Google account id, stored as users.google_id
	Email   string
	Name    string
}

// GoogleVerifier checks ID tokens presented by the Android app.
//
// The web flow cannot be reused: it is an authorization-code exchange that ends
// in a redirect, whereas Android's Credential Manager hands the app a signed ID
// token directly. Only the audience check is subtle — see [Verify].
type GoogleVerifier struct {
	// audience is the OAuth client id the token must be addressed to.
	audience string
}

func NewGoogleVerifier(clientID string) *GoogleVerifier {
	return &GoogleVerifier{audience: strings.TrimSpace(clientID)}
}

func (v *GoogleVerifier) Configured() bool { return v.audience != "" }

// Verify validates the token's signature, issuer, expiry and audience against
// Google's published keys, and returns who it identifies.
//
// The audience is the *web* OAuth client id — the same GOOGLE_CLIENT_ID the
// website already uses. That looks wrong at first glance for an Android
// request, but it is correct: the app calls Credential Manager with
// `setServerClientId(WEB_CLIENT_ID)`, so Google issues a token whose `aud` is
// the web client. The separate Android OAuth client still has to exist, because
// that is what binds the app's package name and signing certificate, but it
// never appears as the audience.
func (v *GoogleVerifier) Verify(ctx context.Context, rawToken string) (GoogleIdentity, error) {
	if !v.Configured() {
		return GoogleIdentity{}, ErrGoogleNotConfigured
	}

	payload, err := idtoken.Validate(ctx, rawToken, v.audience)
	if err != nil {
		return GoogleIdentity{}, fmt.Errorf("auth: invalid google id token: %w", err)
	}

	subject := payload.Subject
	if subject == "" {
		// Without a subject there is no stable key to attach progress to, and
		// falling back to email would silently re-point an account if the
		// reader ever changed it.
		return GoogleIdentity{}, errors.New("auth: google id token has no subject")
	}

	identity := GoogleIdentity{Subject: subject}
	if email, ok := payload.Claims["email"].(string); ok {
		identity.Email = email
	}
	if name, ok := payload.Claims["name"].(string); ok {
		identity.Name = name
	}

	return identity, nil
}
