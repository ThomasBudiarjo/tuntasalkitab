package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"bible-tracker/internal/db"

	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	oauthStateCookie = "bible-tracker-oauth-state"
	oauthStateTTL    = 10 * time.Minute
)

type AuthHandler struct {
	queries     *db.Queries
	store       *sessions.CookieStore
	oauthConfig *oauth2.Config
	// secureCookies marks cookies Secure when the deployment is served over
	// TLS. Inferred from GOOGLE_REDIRECT_URL rather than configured separately,
	// so local development over http still works without another switch.
	secureCookies bool
}

func newOAuthState() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
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
		queries:       queries,
		store:         store,
		oauthConfig:   config,
		secureCookies: strings.HasPrefix(config.RedirectURL, "https://"),
	}
}

func (h *AuthHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	if h.oauthConfig.ClientID == "" {
		http.Error(w, "Google OAuth not configured", http.StatusServiceUnavailable)
		return
	}

	// The state used to be the constant "random-state", which defeats the
	// purpose: an attacker could hand a victim a pre-built callback URL and
	// have the victim's browser complete sign-in against the attacker's Google
	// account. Since the callback merges the visitor's guest progress into the
	// account it lands on, that would quietly hand the victim's reading history
	// to the attacker.
	state, err := newOAuthState()
	if err != nil {
		http.Error(w, "Failed to start sign-in", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		Path:     "/auth",
		MaxAge:   int(oauthStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	url := h.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (h *AuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	// Consume the state cookie whatever happens next, so a failed attempt
	// cannot be replayed.
	expected, stateErr := r.Cookie(oauthStateCookie)
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    "",
		Path:     "/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	got := r.URL.Query().Get("state")
	if stateErr != nil || got == "" ||
		subtle.ConstantTimeCompare([]byte(got), []byte(expected.Value)) != 1 {
		http.Error(w, "Sign-in request could not be verified. Please try again.",
			http.StatusBadRequest)
		return
	}

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
			// The merge used to be fire-and-forget. It also used to be an
			// UPDATE that violated UNIQUE(user_id, day_of_year) whenever both
			// users had touched the same day, so the common case failed
			// silently and the reader lost the progress they had as a guest.
			if err := h.mergeGuestInto(r, anonID, existingUser.ID); err != nil {
				log.Printf("merge guest %d into user %d: %v", anonID, existingUser.ID, err)
				http.Error(w, "Failed to merge progress", http.StatusInternalServerError)
				return
			}
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

// mergeGuestInto folds an anonymous user's progress into a signed-in account
// and removes the guest.
//
// Per day the more recently touched side wins, so signing in never discards
// work done while signed out, and never clobbers newer progress made elsewhere.
// The guest's rows are deleted before the guest itself: reading_progress
// references users(id), and leaving them behind would orphan them.
func (h *AuthHandler) mergeGuestInto(r *http.Request, guestID, userID int64) error {
	if err := h.queries.MergeProgress(r.Context(), guestID, userID); err != nil {
		return fmt.Errorf("copy progress: %w", err)
	}
	if err := h.queries.DeleteProgressForUser(r.Context(), guestID); err != nil {
		return fmt.Errorf("clear guest progress: %w", err)
	}
	if err := h.queries.DeleteUser(r.Context(), guestID); err != nil {
		return fmt.Errorf("delete guest: %w", err)
	}
	return nil
}
