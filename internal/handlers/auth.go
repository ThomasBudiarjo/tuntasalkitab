package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"

	"bible-tracker/internal/db"

	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type AuthHandler struct {
	queries     *db.Queries
	store       *sessions.CookieStore
	oauthConfig *oauth2.Config
}

func NewAuthHandler(queries *db.Queries, store *sessions.CookieStore) *AuthHandler {
	config := &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}

	return &AuthHandler{
		queries:     queries,
		store:       store,
		oauthConfig: config,
	}
}

func (h *AuthHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	if h.oauthConfig.ClientID == "" {
		http.Error(w, "Google OAuth not configured", http.StatusServiceUnavailable)
		return
	}

	state := "random-state" // In production, use a secure random string
	url := h.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (h *AuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	token, err := h.oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
		return
	}

	client := h.oauthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var userInfo struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		http.Error(w, "Failed to decode user info", http.StatusInternalServerError)
		return
	}

	session, _ := h.store.Get(r, "bible-tracker")

	existingUser, err := h.queries.GetUserByGoogleID(r.Context(), sql.NullString{String: userInfo.ID, Valid: true})
	if err == nil {
		if anonID, ok := session.Values["userID"].(int64); ok && anonID != existingUser.ID {
			_ = h.queries.MergeUserProgress(r.Context(), db.MergeUserProgressParams{
				UserID:   existingUser.ID,
				UserID_2: anonID,
			})
			_ = h.queries.DeleteUser(r.Context(), anonID)
		}
		session.Values["userID"] = existingUser.ID
		session.Save(r, w)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	if anonID, ok := session.Values["userID"].(int64); ok {
		err = h.queries.UpdateUserGoogleID(r.Context(), db.UpdateUserGoogleIDParams{
			GoogleID: sql.NullString{String: userInfo.ID, Valid: true},
			Email:    sql.NullString{String: userInfo.Email, Valid: true},
			Name:     sql.NullString{String: userInfo.Name, Valid: true},
			ID:       anonID,
		})
		if err == nil {
			session.Save(r, w)
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
			return
		}
	}

	user, err := h.queries.CreateUser(r.Context(), db.CreateUserParams{
		GoogleID: sql.NullString{String: userInfo.ID, Valid: true},
		Email:    sql.NullString{String: userInfo.Email, Valid: true},
		Name:     sql.NullString{String: userInfo.Name, Valid: true},
	})
	if err != nil {
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	session.Values["userID"] = user.ID
	session.Save(r, w)
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	session, _ := h.store.Get(r, "bible-tracker")
	userID, _ := session.Values["userID"].(int64)

	user, err := h.queries.GetUserByID(r.Context(), userID)
	if err == nil && user.GoogleID.Valid {
		delete(session.Values, "userID")
		session.Save(r, w)
	}

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

