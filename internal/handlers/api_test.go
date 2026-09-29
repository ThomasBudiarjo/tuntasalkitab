package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"bible-tracker/internal/auth"
	"bible-tracker/internal/db"
	"bible-tracker/internal/middleware"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

const testSecret = "a-secret-long-enough-for-testing"

type testAPI struct {
	router     http.Handler
	queries    *db.Queries
	sqlDB      *sql.DB
	signer     *auth.Signer
	apiHandler *APIHandler
}

func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	return newTestAPIWithGoogle(t, auth.NewGoogleVerifier(""))
}

func newTestAPIWithGoogle(t *testing.T, google googleVerifier) *testAPI {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	if err := db.Migrate(context.Background(), sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := sqlDB.Exec("UPDATE sync_clock SET last_ms = 0 WHERE id = 1"); err != nil {
		t.Fatalf("reset sync clock: %v", err)
	}

	queries := db.New(sqlDB)
	signer, err := auth.NewSigner(testSecret)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	apiHandler := NewAPIHandler(queries, signer, google)
	apiAuth := middleware.NewAPIAuth(signer)

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/google", apiHandler.SignInWithGoogle)
		r.Post("/auth/refresh", apiHandler.Refresh)
		r.Get("/plan", apiHandler.GetPlan)
		r.Group(func(r chi.Router) {
			r.Use(apiAuth.Handler)
			r.Get("/progress", apiHandler.GetProgress)
			r.Post("/progress", apiHandler.PushProgress)
		})
	})

	return &testAPI{router: r, queries: queries, sqlDB: sqlDB, signer: signer, apiHandler: apiHandler}
}

func (a *testAPI) do(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	return rec
}

func (a *testAPI) userCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := a.sqlDB.QueryRow("SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	return n
}

func (a *testAPI) tokenFor(t *testing.T, userID int64) string {
	t.Helper()
	token, _ := a.signer.IssueAccess(userID, time.Now())
	return token
}

// The regression that motivated putting the API outside the session middleware.
//
// SessionMiddleware creates an anonymous users row for every request that
// arrives without a session cookie. That is right for a browser and disastrous
// for an API, where every unauthenticated probe and every retry with a stale
// token would leave a row behind.
func TestUnauthenticatedRequestsCreateNoUsers(t *testing.T) {
	api := newTestAPI(t)
	before := api.userCount(t)

	cases := []struct {
		method, path, token string
	}{
		{http.MethodGet, "/api/v1/progress", ""},
		{http.MethodGet, "/api/v1/progress", "garbage"},
		{http.MethodPost, "/api/v1/progress", ""},
		{http.MethodGet, "/api/v1/progress", "a.b"},
	}
	for _, c := range cases {
		rec := api.do(t, c.method, c.path, c.token, map[string]any{})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with token %q: status %d, want 401", c.method, c.path, c.token, rec.Code)
		}
	}

	if after := api.userCount(t); after != before {
		t.Errorf("unauthenticated requests created %d users", after-before)
	}
}

func TestProgressRoundTrip(t *testing.T) {
	api := newTestAPI(t)
	user, err := api.queries.CreateUser(context.Background(), db.CreateUserParams{
		GoogleID: sql.NullString{String: "g-1", Valid: true},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := api.tokenFor(t, user.ID)

	api.apiHandler.now = func() time.Time { return time.UnixMilli(1_000) }
	rec := api.do(t, http.MethodPost, "/api/v1/progress", token, map[string]any{
		"items": []map[string]any{
			{"dayOfYear": 1, "completed": true, "updatedAt": 1000},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("push day 1: status %d body %s", rec.Code, rec.Body.String())
	}

	api.apiHandler.now = func() time.Time { return time.UnixMilli(2_000) }
	rec = api.do(t, http.MethodPost, "/api/v1/progress", token, map[string]any{
		"items": []map[string]any{
			{"dayOfYear": 2, "completed": true, "updatedAt": 2000},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("push day 2: status %d body %s", rec.Code, rec.Body.String())
	}

	var pushed progressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pushed); err != nil {
		t.Fatalf("decode push response: %v", err)
	}
	if len(pushed.Items) != 1 {
		t.Fatalf("push echoed %d items, want 1", len(pushed.Items))
	}
	if pushed.ServerTime <= 0 {
		t.Error("push did not return a server time to use as the next cursor")
	}

	// Pull everything.
	api.apiHandler.now = func() time.Time { return time.UnixMilli(3_000) }
	rec = api.do(t, http.MethodGet, "/api/v1/progress?since=0", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("pull: status %d body %s", rec.Code, rec.Body.String())
	}
	var pulled progressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pulled); err != nil {
		t.Fatalf("decode pull: %v", err)
	}
	if len(pulled.Items) != 2 {
		t.Fatalf("pull returned %d items, want 2", len(pulled.Items))
	}

	// A delta pull past the first server-side change cursor returns only the second.
	rec = api.do(t, http.MethodGet, "/api/v1/progress?since=1000", token, nil)
	var delta progressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &delta); err != nil {
		t.Fatalf("decode delta: %v", err)
	}
	if len(delta.Items) != 1 || delta.Items[0].DayOfYear != 2 {
		t.Errorf("delta since=1000 returned %+v, want only day 2", delta.Items)
	}
}

func TestDeltaPullUsesServerChangeCursorNotDeviceUpdatedAt(t *testing.T) {
	api := newTestAPI(t)
	ctx := context.Background()
	user, err := api.queries.CreateUser(ctx, db.CreateUserParams{
		GoogleID: sql.NullString{String: "g-slow-clock", Valid: true},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := api.tokenFor(t, user.ID)

	// Device B pulls when the server clock is far ahead of device A's clock.
	api.apiHandler.now = func() time.Time { return time.UnixMilli(100_000) }
	rec := api.do(t, http.MethodGet, "/api/v1/progress?since=0", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("initial pull: %d %s", rec.Code, rec.Body.String())
	}
	var initial progressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode initial pull: %v", err)
	}
	if initial.ServerTime != 100_000 {
		t.Fatalf("initial cursor = %d, want 100000", initial.ServerTime)
	}

	// Later, device A uploads a valid change stamped with its own slow/offline
	// clock. LWW still uses updatedAt=5000, but delta sync must use the server
	// change time so device B can see it after since=100000.
	api.apiHandler.now = func() time.Time { return time.UnixMilli(101_000) }
	rec = api.do(t, http.MethodPost, "/api/v1/progress", token, map[string]any{
		"items": []map[string]any{{"dayOfYear": 7, "completed": true, "updatedAt": 5_000}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("slow-clock push: %d %s", rec.Code, rec.Body.String())
	}

	api.apiHandler.now = func() time.Time { return time.UnixMilli(102_000) }
	rec = api.do(t, http.MethodGet, fmt.Sprintf("/api/v1/progress?since=%d", initial.ServerTime), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delta pull: %d %s", rec.Code, rec.Body.String())
	}
	var delta progressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &delta); err != nil {
		t.Fatalf("decode delta: %v", err)
	}
	if len(delta.Items) != 1 || delta.Items[0].DayOfYear != 7 || delta.Items[0].UpdatedAt != 5_000 {
		t.Fatalf("delta after slow-clock push = %+v, want day 7 updatedAt 5000", delta.Items)
	}
}

func TestDeltaCursorDoesNotSkipSameMillisecondPushAfterPull(t *testing.T) {
	api := newTestAPI(t)
	user, err := api.queries.CreateUser(context.Background(), db.CreateUserParams{
		GoogleID: sql.NullString{String: "g-same-ms-pull", Valid: true},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := api.tokenFor(t, user.ID)

	api.apiHandler.now = func() time.Time { return time.UnixMilli(100_000) }
	rec := api.do(t, http.MethodGet, "/api/v1/progress?since=0", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("initial pull: %d %s", rec.Code, rec.Body.String())
	}
	var initial progressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode initial pull: %v", err)
	}

	api.apiHandler.now = func() time.Time { return time.UnixMilli(100_000) }
	rec = api.do(t, http.MethodPost, "/api/v1/progress", token, map[string]any{
		"items": []map[string]any{{"dayOfYear": 8, "completed": true, "updatedAt": 8_000}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("same-ms push: %d %s", rec.Code, rec.Body.String())
	}

	api.apiHandler.now = func() time.Time { return time.UnixMilli(100_001) }
	rec = api.do(t, http.MethodGet, fmt.Sprintf("/api/v1/progress?since=%d", initial.ServerTime), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delta pull: %d %s", rec.Code, rec.Body.String())
	}
	var delta progressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &delta); err != nil {
		t.Fatalf("decode delta: %v", err)
	}
	if len(delta.Items) != 1 || delta.Items[0].DayOfYear != 8 {
		t.Fatalf("delta after same-ms push = %+v, want day 8", delta.Items)
	}
}

func TestPushCursorDoesNotSkipPeerChangeInSameMillisecond(t *testing.T) {
	api := newTestAPI(t)
	user, err := api.queries.CreateUser(context.Background(), db.CreateUserParams{
		GoogleID: sql.NullString{String: "g-same-ms-push", Valid: true},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := api.tokenFor(t, user.ID)

	api.apiHandler.now = func() time.Time { return time.UnixMilli(200_000) }
	rec := api.do(t, http.MethodPost, "/api/v1/progress", token, map[string]any{
		"items": []map[string]any{{"dayOfYear": 9, "completed": true, "updatedAt": 9_000}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("first push: %d %s", rec.Code, rec.Body.String())
	}
	var first progressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first push: %v", err)
	}

	api.apiHandler.now = func() time.Time { return time.UnixMilli(200_000) }
	rec = api.do(t, http.MethodPost, "/api/v1/progress", token, map[string]any{
		"items": []map[string]any{{"dayOfYear": 10, "completed": true, "updatedAt": 10_000}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("peer push: %d %s", rec.Code, rec.Body.String())
	}

	api.apiHandler.now = func() time.Time { return time.UnixMilli(200_001) }
	rec = api.do(t, http.MethodGet, fmt.Sprintf("/api/v1/progress?since=%d", first.ServerTime), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delta pull: %d %s", rec.Code, rec.Body.String())
	}
	var delta progressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &delta); err != nil {
		t.Fatalf("decode delta: %v", err)
	}
	if len(delta.Items) != 1 || delta.Items[0].DayOfYear != 10 {
		t.Fatalf("delta after peer same-ms push = %+v, want day 10", delta.Items)
	}
}

// A device that was offline holds stale state. When it finally pushes, it must
// not undo a newer change made elsewhere — and it has to be told it lost.
func TestPushResolvesConflictsLastWriteWins(t *testing.T) {
	api := newTestAPI(t)
	user, err := api.queries.CreateUser(context.Background(), db.CreateUserParams{
		GoogleID: sql.NullString{String: "g-2", Valid: true},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := api.tokenFor(t, user.ID)

	// The website ticks day 5 at t=5000.
	rec := api.do(t, http.MethodPost, "/api/v1/progress", token, map[string]any{
		"items": []map[string]any{{"dayOfYear": 5, "completed": true, "updatedAt": 5000}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
	}

	// A phone that has been offline pushes an older un-tick.
	rec = api.do(t, http.MethodPost, "/api/v1/progress", token, map[string]any{
		"items": []map[string]any{{"dayOfYear": 5, "completed": false, "updatedAt": 3000}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("stale push: %d %s", rec.Code, rec.Body.String())
	}

	var echoed progressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &echoed); err != nil {
		t.Fatalf("decode echo: %v", err)
	}
	if len(echoed.Items) != 1 {
		t.Fatalf("echo had %d items, want 1", len(echoed.Items))
	}
	// The echo is how the client learns its write lost and reconciles.
	if !echoed.Items[0].Completed || echoed.Items[0].UpdatedAt != 5000 {
		t.Errorf("stale push won: echo = %+v, want completed at 5000", echoed.Items[0])
	}
}

func TestPushValidatesInput(t *testing.T) {
	api := newTestAPI(t)
	user, err := api.queries.CreateUser(context.Background(), db.CreateUserParams{
		GoogleID: sql.NullString{String: "g-3", Valid: true},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := api.tokenFor(t, user.ID)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"day zero", map[string]any{"items": []map[string]any{
			{"dayOfYear": 0, "completed": true, "updatedAt": 1}}}},
		{"day past the plan", map[string]any{"items": []map[string]any{
			{"dayOfYear": 366, "completed": true, "updatedAt": 1}}}},
		{"missing updatedAt", map[string]any{"items": []map[string]any{
			{"dayOfYear": 5, "completed": true}}}},
		{"unknown field", map[string]any{"item": []map[string]any{}}},
	}
	for _, c := range cases {
		rec := api.do(t, http.MethodPost, "/api/v1/progress", token, c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (body %s)", c.name, rec.Code, rec.Body.String())
		}
	}

	// Oversized batches are refused rather than absorbed.
	items := make([]map[string]any, 0, maxProgressItems+1)
	for i := range maxProgressItems + 1 {
		items = append(items, map[string]any{"dayOfYear": (i % 365) + 1, "completed": true, "updatedAt": 1})
	}
	rec := api.do(t, http.MethodPost, "/api/v1/progress", token, map[string]any{"items": items})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("oversized batch: status %d, want 400", rec.Code)
	}
}

// One reader must never see another's progress.
func TestProgressIsScopedToTheAuthenticatedUser(t *testing.T) {
	api := newTestAPI(t)
	ctx := context.Background()

	alice, err := api.queries.CreateUser(ctx, db.CreateUserParams{GoogleID: sql.NullString{String: "g-a", Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := api.queries.CreateUser(ctx, db.CreateUserParams{GoogleID: sql.NullString{String: "g-b", Valid: true}})
	if err != nil {
		t.Fatal(err)
	}

	rec := api.do(t, http.MethodPost, "/api/v1/progress", api.tokenFor(t, alice.ID), map[string]any{
		"items": []map[string]any{{"dayOfYear": 100, "completed": true, "updatedAt": 1000}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("alice push: %d", rec.Code)
	}

	rec = api.do(t, http.MethodGet, "/api/v1/progress?since=0", api.tokenFor(t, bob.ID), nil)
	var bobsProgress progressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &bobsProgress); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(bobsProgress.Items) != 0 {
		t.Errorf("bob can see %d of alice's rows", len(bobsProgress.Items))
	}
}

func TestRefreshRotatesAndCannotBeReused(t *testing.T) {
	api := newTestAPI(t)
	ctx := context.Background()
	user, err := api.queries.CreateUser(ctx, db.CreateUserParams{GoogleID: sql.NullString{String: "g-r", Valid: true}})
	if err != nil {
		t.Fatal(err)
	}

	refreshToken, hash, err := auth.NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := api.queries.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		TokenHash: hash,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	rec := api.do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refreshToken": refreshToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body.String())
	}
	var issued authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if issued.AccessToken == "" || issued.RefreshToken == "" {
		t.Fatal("refresh returned an empty token")
	}
	if issued.RefreshToken == refreshToken {
		t.Error("refresh token was not rotated")
	}
	if issued.User.ID != user.ID {
		t.Errorf("issued for user %d, want %d", issued.User.ID, user.ID)
	}

	// The access token it returned must actually work.
	rec = api.do(t, http.MethodGet, "/api/v1/progress?since=0", issued.AccessToken, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("issued access token rejected: %d", rec.Code)
	}

	// Replaying the spent token fails, which is what makes a leaked one
	// useful at most once.
	rec = api.do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refreshToken": refreshToken})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("replayed refresh token: status %d, want 401", rec.Code)
	}
}

func TestRefreshRejectsExpiredTokens(t *testing.T) {
	api := newTestAPI(t)
	ctx := context.Background()
	user, err := api.queries.CreateUser(ctx, db.CreateUserParams{GoogleID: sql.NullString{String: "g-e", Valid: true}})
	if err != nil {
		t.Fatal(err)
	}

	refreshToken, hash, err := auth.NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := api.queries.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		TokenHash: hash,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	rec := api.do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refreshToken": refreshToken})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expired refresh token: status %d, want 401", rec.Code)
	}
}

func TestSignInRequiresGoogleConfiguration(t *testing.T) {
	api := newTestAPI(t) // built with an empty GOOGLE_CLIENT_ID

	rec := api.do(t, http.MethodPost, "/api/v1/auth/google", "", map[string]any{"idToken": "anything"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503 when google sign-in is unconfigured", rec.Code)
	}

	rec = api.do(t, http.MethodPost, "/api/v1/auth/google", "", map[string]any{"idToken": ""})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400 for an empty idToken", rec.Code)
	}
}

type fakeGoogleVerifier struct {
	identity auth.GoogleIdentity
	err      error
	calls    int
}

func (v *fakeGoogleVerifier) Verify(ctx context.Context, rawToken string) (auth.GoogleIdentity, error) {
	v.calls++
	if v.err != nil {
		return auth.GoogleIdentity{}, v.err
	}
	return v.identity, nil
}

func TestSignInWithGoogleIssuesTokensAndReusesExistingUser(t *testing.T) {
	verifier := &fakeGoogleVerifier{identity: auth.GoogleIdentity{
		Subject: "google-subject-1",
		Email:   "reader@example.com",
		Name:    "Reader",
	}}
	api := newTestAPIWithGoogle(t, verifier)

	rec := api.do(t, http.MethodPost, "/api/v1/auth/google", "", map[string]any{"idToken": "id-token"})
	if rec.Code != http.StatusOK {
		t.Fatalf("first sign-in: %d %s", rec.Code, rec.Body.String())
	}
	var first authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if first.AccessToken == "" || first.RefreshToken == "" {
		t.Fatal("first sign-in returned empty tokens")
	}
	if first.User.Email != "reader@example.com" || first.User.Name != "Reader" {
		t.Fatalf("first sign-in user = %+v", first.User)
	}

	rec = api.do(t, http.MethodGet, "/api/v1/progress?since=0", first.AccessToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("access token from sign-in was rejected: %d %s", rec.Code, rec.Body.String())
	}

	rec = api.do(t, http.MethodPost, "/api/v1/auth/google", "", map[string]any{"idToken": "id-token-again"})
	if rec.Code != http.StatusOK {
		t.Fatalf("second sign-in: %d %s", rec.Code, rec.Body.String())
	}
	var second authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if second.User.ID != first.User.ID {
		t.Fatalf("second sign-in created user %d, want existing user %d", second.User.ID, first.User.ID)
	}
	if got := api.userCount(t); got != 1 {
		t.Fatalf("sign-in created %d users, want 1", got)
	}
	if verifier.calls != 2 {
		t.Fatalf("verifier called %d times, want 2", verifier.calls)
	}
}

func TestConcurrentFirstGoogleSignInReusesRacingUser(t *testing.T) {
	verifier := &fakeGoogleVerifier{identity: auth.GoogleIdentity{
		Subject: "google-race",
		Email:   "race@example.com",
		Name:    "Race Reader",
	}}
	api := newTestAPIWithGoogle(t, verifier)

	const attempts = 8
	type result struct {
		status int
		body   string
	}
	results := make(chan result, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := api.do(t, http.MethodPost, "/api/v1/auth/google", "", map[string]any{"idToken": "id-token"})
			results <- result{status: rec.Code, body: rec.Body.String()}
		}()
	}
	wg.Wait()
	close(results)

	for result := range results {
		if result.status != http.StatusOK {
			t.Fatalf("concurrent sign-in status %d body %s, want 200", result.status, result.body)
		}
	}
	if got := api.userCount(t); got != 1 {
		t.Fatalf("concurrent sign-ins created %d users, want 1", got)
	}
}

func TestRejectsTrailingJSON(t *testing.T) {
	api := newTestAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/google", bytes.NewBufferString(`{"idToken":"x"} {}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	api.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 for trailing JSON", rec.Code)
	}
}

// The plan endpoint is what lets a correction reach installed apps without an
// app store release, so it has to be readable without signing in and cheap to
// re-check.
func TestPlanIsPublicAndCacheable(t *testing.T) {
	api := newTestAPI(t)

	rec := api.do(t, http.MethodGet, "/api/v1/plan", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("plan: status %d", rec.Code)
	}

	var plan struct {
		Version string            `json:"version"`
		Days    map[string]string `json:"days"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if len(plan.Days) != 365 {
		t.Errorf("plan has %d days, want 365", len(plan.Days))
	}
	if plan.Version == "" {
		t.Error("plan has no version for the client to compare against")
	}
	if plan.Days["1"] == "" {
		t.Error("day 1 is empty")
	}

	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("plan response has no ETag")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plan", nil)
	req.Header.Set("If-None-Match", etag)
	cached := httptest.NewRecorder()
	api.router.ServeHTTP(cached, req)
	if cached.Code != http.StatusNotModified {
		t.Errorf("re-fetch with ETag: status %d, want 304", cached.Code)
	}
	if cached.Body.Len() != 0 {
		t.Errorf("304 response carried %d bytes of body", cached.Body.Len())
	}
}

func TestPushRejectsMalformedSince(t *testing.T) {
	api := newTestAPI(t)
	user, err := api.queries.CreateUser(context.Background(), db.CreateUserParams{
		GoogleID: sql.NullString{String: "g-s", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	token := api.tokenFor(t, user.ID)

	for _, since := range []string{"yesterday", "-1", "1.5"} {
		rec := api.do(t, http.MethodGet, fmt.Sprintf("/api/v1/progress?since=%s", since), token, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("since=%q: status %d, want 400", since, rec.Code)
		}
	}
}
