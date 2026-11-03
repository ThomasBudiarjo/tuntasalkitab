package handlers

import (
	"database/sql"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bible-tracker/internal/db"
	"bible-tracker/internal/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	_ "modernc.org/sqlite"
)

type accountFixture struct {
	sqlDB   *sql.DB
	queries *db.Queries
	router  http.Handler
	cookie  *http.Cookie
	userID  int64
	otherID int64
}

func newAccountFixture(t *testing.T) *accountFixture {
	t.Helper()
	schema, err := os.ReadFile("../../schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if _, err := sqlDB.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	templates, err := template.ParseFS(os.DirFS("../../templates"), "*.html", "partials/*.html")
	if err != nil {
		t.Fatal(err)
	}

	queries := db.New(sqlDB)
	store := sessions.NewCookieStore([]byte("test-session-secret-0123456789"))
	f := &accountFixture{sqlDB: sqlDB, queries: queries}

	for i, email := range []string{"reader@example.com", "other@example.com"} {
		u, err := queries.CreateUser(t.Context(), db.CreateUserParams{
			GoogleID: sql.NullString{String: "google-" + email, Valid: true},
			Email:    sql.NullString{String: email, Valid: true},
			Name:     sql.NullString{String: email, Valid: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		for day := int64(1); day <= 3; day++ {
			if err := queries.UpsertProgress(t.Context(), db.UpsertProgressParams{UserID: u.ID, DayOfYear: day, Completed: sql.NullBool{Bool: true, Valid: true}}); err != nil {
				t.Fatal(err)
			}
		}
		if i == 0 {
			f.userID = u.ID
		} else {
			f.otherID = u.ID
		}
	}

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.NewSessionMiddleware(store, queries).Handler)
		account := NewAccountHandler(sqlDB, queries, store, templates)
		r.Get("/hapus-akun", account.DeletePage)
		r.Post("/hapus-akun", account.Delete)
	})
	f.router = r

	// Sign the reader in the way GoogleCallback does: userID in the session.
	rec := httptest.NewRecorder()
	if err := middleware.SetUserID(rec, httptest.NewRequest("GET", "/", nil), store, f.userID); err != nil {
		t.Fatal(err)
	}
	f.cookie = rec.Result().Cookies()[0]
	return f
}

// do serves req and, when it carried the reader's cookie, keeps any updated one.
func (f *accountFixture) do(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if _, err := req.Cookie(f.cookie.Name); err != nil {
		return rec
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == f.cookie.Name {
			f.cookie = c
		}
	}
	return rec
}

func (f *accountFixture) post(t *testing.T, token string, withCookie bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/hapus-akun", strings.NewReader(url.Values{"csrf_token": {token}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if withCookie {
		req.AddCookie(f.cookie)
	}
	return f.do(t, req)
}

func (f *accountFixture) counts(t *testing.T, userID int64) (users, progress int) {
	t.Helper()
	if err := f.sqlDB.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, userID).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := f.sqlDB.QueryRow(`SELECT COUNT(*) FROM reading_progress WHERE user_id = ?`, userID).Scan(&progress); err != nil {
		t.Fatal(err)
	}
	return users, progress
}

var csrfField = regexp.MustCompile(`name="csrf_token" value="([0-9a-f]+)"`)

func TestDeleteAccount(t *testing.T) {
	f := newAccountFixture(t)

	req := httptest.NewRequest("GET", "/hapus-akun", nil)
	req.AddCookie(f.cookie)
	page := f.do(t, req)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "reader@example.com") {
		t.Fatalf("GET /hapus-akun: %d, want the signed-in email in the page", page.Code)
	}
	m := csrfField.FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatal("GET /hapus-akun: no csrf_token field in the form")
	}
	token := m[1]

	// A cross-site form post: the browser sends the session cookie (worst
	// case, SameSite ignored) but the attacker cannot know the token.
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"missing token": f.post(t, "", true),
		"wrong token":   f.post(t, strings.Repeat("0", len(token)), true),
		"no session":    f.post(t, token, false),
	} {
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, rec.Code)
		}
		if users, progress := f.counts(t, f.userID); users != 1 || progress != 3 {
			t.Fatalf("%s: user rows %d, progress rows %d; want the account untouched", name, users, progress)
		}
	}

	rec := f.post(t, token, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Akun Anda sudah dihapus") {
		t.Fatalf("signed-in POST: status %d, want the confirmation page", rec.Code)
	}
	if users, progress := f.counts(t, f.userID); users != 0 || progress != 0 {
		t.Fatalf("after delete: user rows %d, progress rows %d; want 0 and 0", users, progress)
	}
	if users, progress := f.counts(t, f.otherID); users != 1 || progress != 3 {
		t.Fatalf("other account: user rows %d, progress rows %d; want it untouched", users, progress)
	}
	if f.cookie.MaxAge >= 0 {
		t.Fatalf("session cookie MaxAge %d, want it cleared", f.cookie.MaxAge)
	}
}
