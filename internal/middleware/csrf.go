package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/gorilla/sessions"
)

type contextKey string

const CSRFTokenKey contextKey = "csrfToken"

type CSRFMiddleware struct {
	store *sessions.CookieStore
}

func NewCSRFMiddleware(store *sessions.CookieStore) *CSRFMiddleware {
	return &CSRFMiddleware{store: store}
}

func (m *CSRFMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, _ := m.store.Get(r, "bible-tracker")

		token, ok := session.Values["csrfToken"].(string)
		if !ok || token == "" {
			token = generateToken()
			session.Values["csrfToken"] = token
			session.Save(r, w)
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			requestToken := r.Header.Get("X-CSRF-Token")
			if requestToken == "" {
				requestToken = r.FormValue("csrf_token")
			}

			if requestToken != token {
				http.Error(w, "Forbidden - Invalid CSRF token", http.StatusForbidden)
				return
			}
		}

		ctx := context.WithValue(r.Context(), CSRFTokenKey, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func GetCSRFToken(r *http.Request) string {
	if token, ok := r.Context().Value(CSRFTokenKey).(string); ok {
		return token
	}
	return ""
}
