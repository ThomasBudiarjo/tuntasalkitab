package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"bible-tracker/internal/auth"
)

// apiUserKey is a package-private type so nothing outside this file can collide
// with it. The session middleware uses a bare string key, which any package
// could overwrite by accident.
type apiUserKey struct{}

// APIAuth authenticates /api/v1 requests from a bearer access token.
//
// This deliberately does not reuse [SessionMiddleware]. That one creates an
// anonymous users row for every request without a session, which is right for a
// browser landing on the site and very wrong for an API: every unauthenticated
// call — including every probe and every retry with an expired token — would
// insert a row that nothing ever reads again.
type APIAuth struct {
	signer *auth.Signer
	now    func() time.Time
}

func NewAPIAuth(signer *auth.Signer) *APIAuth {
	return &APIAuth{signer: signer, now: time.Now}
}

func (a *APIAuth) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			writeAuthError(w, "missing bearer token")
			return
		}

		userID, err := a.signer.VerifyAccess(strings.TrimSpace(token), a.now())
		if err != nil {
			// The client distinguishes only "refresh and retry" from "sign in
			// again", and both are 401; saying which check failed would only
			// help someone probing.
			writeAuthError(w, "invalid or expired access token")
			return
		}

		ctx := context.WithValue(r.Context(), apiUserKey{}, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// APIUserID returns the authenticated user for a request that passed through
// [APIAuth.Handler].
func APIUserID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(apiUserKey{}).(int64)
	return id, ok
}

func writeAuthError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="tuntasalkitab"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
