package middleware

import (
	"context"
	"database/sql"
	"net/http"

	"bible-tracker/internal/db"

	"github.com/gorilla/sessions"
)

type SessionMiddleware struct {
	store   *sessions.CookieStore
	queries *db.Queries
}

func NewSessionMiddleware(store *sessions.CookieStore, queries *db.Queries) *SessionMiddleware {
	return &SessionMiddleware{
		store:   store,
		queries: queries,
	}
}

func (m *SessionMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, _ := m.store.Get(r, "bible-tracker")

		var userID int64
		if id, ok := session.Values["userID"].(int64); ok {
			userID = id
		} else {
			user, err := m.queries.CreateAnonymousUser(r.Context())
			if err != nil {
				http.Error(w, "Failed to create session", http.StatusInternalServerError)
				return
			}
			userID = user.ID
			session.Values["userID"] = userID
			session.Save(r, w)
		}

		ctx := context.WithValue(r.Context(), "userID", userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetUserFromSession(r *http.Request, store *sessions.CookieStore, queries *db.Queries) (db.User, error) {
	session, _ := store.Get(r, "bible-tracker")
	if id, ok := session.Values["userID"].(int64); ok {
		return queries.GetUserByID(r.Context(), id)
	}
	return db.User{}, sql.ErrNoRows
}

func SetUserID(w http.ResponseWriter, r *http.Request, store *sessions.CookieStore, userID int64) error {
	session, _ := store.Get(r, "bible-tracker")
	session.Values["userID"] = userID
	return session.Save(r, w)
}

