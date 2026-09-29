package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"bible-tracker/internal/auth"
	"bible-tracker/internal/db"
	"bible-tracker/internal/middleware"
	"bible-tracker/internal/reading"
)

type googleVerifier interface {
	Verify(ctx context.Context, rawToken string) (auth.GoogleIdentity, error)
}

// maxProgressItems caps a single push. The plan is 365 days, so a client with
// anything to say can say it in one request; a larger body is a mistake or an
// abuse, and either way is better refused than absorbed.
const maxProgressItems = 400

// maxRequestBytes bounds how much JSON is read before giving up.
const maxRequestBytes = 1 << 20 // 1 MiB

// APIHandler serves the JSON API used by the Android app.
//
// The website's handlers render HTML fragments against a session cookie. A
// mobile client cannot hold that cookie across Google sign-in, so these
// endpoints authenticate with a bearer token instead and speak JSON.
type APIHandler struct {
	queries  *db.Queries
	signer   *auth.Signer
	google   googleVerifier
	now      func() time.Time
	planEtag string
}

func NewAPIHandler(queries *db.Queries, signer *auth.Signer, google googleVerifier) *APIHandler {
	return &APIHandler{
		queries:  queries,
		signer:   signer,
		google:   google,
		now:      time.Now,
		planEtag: `"` + reading.PlanVersion() + `"`,
	}
}

// --- wire types ---

type progressItem struct {
	DayOfYear int  `json:"dayOfYear"`
	Completed bool `json:"completed"`
	// Epoch milliseconds of the change, as the writing device saw it.
	UpdatedAt int64 `json:"updatedAt"`
}

type progressResponse struct {
	// ServerTime is the cursor to send as `since` on the next pull. It comes
	// from the server so a device with a wrong clock cannot skip changes.
	ServerTime int64          `json:"serverTime"`
	Items      []progressItem `json:"items"`
}

type authResponse struct {
	AccessToken  string   `json:"accessToken"`
	RefreshToken string   `json:"refreshToken"`
	ExpiresAt    int64    `json:"expiresAt"`
	User         userInfo `json:"user"`
}

type userInfo struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// --- auth ---

// SignInWithGoogle exchanges a Google ID token for this API's own tokens.
func (h *APIHandler) SignInWithGoogle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDToken string `json:"idToken"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.IDToken == "" {
		writeError(w, http.StatusBadRequest, "idToken is required")
		return
	}

	identity, err := h.google.Verify(r.Context(), body.IDToken)
	if err != nil {
		if errors.Is(err, auth.ErrGoogleNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "google sign-in is not configured")
			return
		}
		writeError(w, http.StatusUnauthorized, "google id token rejected")
		return
	}

	user, err := h.findOrCreateUser(r, identity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not sign in")
		return
	}

	h.issueTokens(w, r, user)
}

func (h *APIHandler) findOrCreateUser(r *http.Request, identity auth.GoogleIdentity) (db.User, error) {
	return h.queries.UpsertGoogleUser(r.Context(), db.CreateUserParams{
		GoogleID: sql.NullString{String: identity.Subject, Valid: true},
		Email:    sql.NullString{String: identity.Email, Valid: identity.Email != ""},
		Name:     sql.NullString{String: identity.Name, Valid: identity.Name != ""},
	})
}

// Refresh rotates a refresh token for a new pair.
//
// The presented token is deleted whether or not the new one is issued, so a
// token that leaks is useful at most once — and a second use of an already
// spent token fails, which is the signal that something is wrong.
func (h *APIHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refreshToken"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refreshToken is required")
		return
	}

	hash := auth.HashRefreshToken(body.RefreshToken)
	stored, err := h.queries.ConsumeRefreshTokenWithRetry(r.Context(), hash)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "refresh token rejected")
		return
	}

	if h.now().After(stored.ExpiresAt) {
		writeError(w, http.StatusUnauthorized, "refresh token expired")
		return
	}

	user, err := h.queries.GetUserByID(r.Context(), stored.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "refresh token rejected")
		return
	}

	h.issueTokens(w, r, user)
}

func (h *APIHandler) issueTokens(w http.ResponseWriter, r *http.Request, user db.User) {
	now := h.now()
	accessToken, expiry := h.signer.IssueAccess(user.ID, now)

	refreshToken, refreshHash, err := auth.NewRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue tokens")
		return
	}

	if err := h.queries.CreateRefreshTokenWithRetry(r.Context(), db.CreateRefreshTokenParams{
		TokenHash: refreshHash,
		UserID:    user.ID,
		ExpiresAt: now.Add(auth.RefreshTokenTTL),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue tokens")
		return
	}

	// Opportunistic cleanup; a failure here costs nothing but a stale row.
	_ = h.queries.DeleteExpiredRefreshTokens(r.Context())

	writeJSON(w, http.StatusOK, authResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiry.UnixMilli(),
		User: userInfo{
			ID:    user.ID,
			Email: user.Email.String,
			Name:  user.Name.String,
		},
	})
}

// --- progress ---

// GetProgress returns rows changed after the `since` cursor (epoch millis).
func (h *APIHandler) GetProgress(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.APIUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	since := int64(0)
	if raw := r.URL.Query().Get("since"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "since must be epoch milliseconds")
			return
		}
		since = parsed
	}

	cutoff, err := h.queries.NextSyncCursor(r.Context(), h.now().UnixMilli())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not allocate sync cursor")
		return
	}
	rows, err := h.queries.GetProgressSince(r.Context(), db.GetProgressSinceParams{
		UserID:      userID,
		ChangedAt:   sql.NullInt64{Int64: since, Valid: true},
		ChangedAt_2: sql.NullInt64{Int64: cutoff, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read progress")
		return
	}

	writeJSON(w, http.StatusOK, progressResponse{
		ServerTime: cutoff,
		Items:      toItems(rows),
	})
}

// PushProgress applies a batch of local changes and echoes the authoritative
// state of exactly the days that were sent.
//
// Conflicts resolve last-write-wins on updatedAt, with ties going to whatever
// the server already holds. The echo is what lets the client clear its pending
// flags: it learns not just that the push landed, but which of its rows lost.
func (h *APIHandler) PushProgress(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.APIUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var body struct {
		Items []progressItem `json:"items"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.Items) > maxProgressItems {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("at most %d items per request", maxProgressItems))
		return
	}

	touched := make(map[int]bool, len(body.Items))
	for _, item := range body.Items {
		if item.DayOfYear < 1 || item.DayOfYear > 365 {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("dayOfYear %d is outside 1..365", item.DayOfYear))
			return
		}
		if item.UpdatedAt <= 0 {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("day %d has no updatedAt", item.DayOfYear))
			return
		}
		touched[item.DayOfYear] = true
	}

	changedAt, err := h.queries.NextSyncCursor(r.Context(), h.now().UnixMilli())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not allocate sync cursor")
		return
	}
	for _, item := range body.Items {
		var completedAt sql.NullTime
		if item.Completed {
			completedAt = sql.NullTime{Time: time.UnixMilli(item.UpdatedAt).UTC(), Valid: true}
		}

		if err := h.queries.UpsertProgressIfNewer(r.Context(), db.UpsertProgressIfNewerParams{
			UserID:      userID,
			DayOfYear:   int64(item.DayOfYear),
			Completed:   sql.NullBool{Bool: item.Completed, Valid: true},
			CompletedAt: completedAt,
			UpdatedAt:   sql.NullInt64{Int64: item.UpdatedAt, Valid: true},
			ChangedAt:   sql.NullInt64{Int64: changedAt, Valid: true},
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save progress")
			return
		}
	}

	rows, err := h.queries.GetProgress(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read progress")
		return
	}

	echo := make([]db.ReadingProgress, 0, len(touched))
	for _, row := range rows {
		if touched[int(row.DayOfYear)] {
			echo = append(echo, row)
		}
	}

	writeJSON(w, http.StatusOK, progressResponse{
		ServerTime: changedAt,
		Items:      toItems(echo),
	})
}

// --- plan ---

// GetPlan serves the reading plan so a correction reaches installed apps
// without waiting on an app store release.
func (h *APIHandler) GetPlan(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("ETag", h.planEtag)
	w.Header().Set("Cache-Control", "public, max-age=3600")

	if match := r.Header.Get("If-None-Match"); match == h.planEtag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	writeJSON(w, http.StatusOK, struct {
		Version string            `json:"version"`
		Days    map[string]string `json:"days"`
	}{
		Version: reading.PlanVersion(),
		Days:    reading.Days(),
	})
}

// --- helpers ---

func toItems(rows []db.ReadingProgress) []progressItem {
	items := make([]progressItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, progressItem{
			DayOfYear: int(row.DayOfYear),
			Completed: row.Completed.Bool,
			UpdatedAt: row.UpdatedAt.Int64,
		})
	}
	return items
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid JSON")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request body must contain a single JSON value")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
