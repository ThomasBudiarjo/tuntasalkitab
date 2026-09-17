package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"bible-tracker/internal/auth"
	"bible-tracker/internal/db"
	"bible-tracker/internal/handlers"
	"bible-tracker/internal/middleware"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

//go:embed templates/*.html templates/partials/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

const (
	databasePathEnv     = "DATABASE_PATH"
	tursoDatabaseURLEnv = "TURSO_DATABASE_URL"
	tursoAuthTokenEnv   = "TURSO_AUTH_TOKEN"

	defaultDatabasePath = "bible-tracker.db"
	sqliteDriver        = "sqlite"
	libSQLDriver        = "libsql"
)

type databaseConfig struct {
	driver string
	dsn    string
}

func loadDatabaseConfig() (databaseConfig, error) {
	tursoURL := strings.TrimSpace(os.Getenv(tursoDatabaseURLEnv))
	tursoToken := strings.TrimSpace(os.Getenv(tursoAuthTokenEnv))

	if tursoURL != "" {
		if tursoToken == "" {
			return databaseConfig{}, fmt.Errorf("%s is required when %s is set", tursoAuthTokenEnv, tursoDatabaseURLEnv)
		}

		dsn, err := buildTursoDSN(tursoURL, tursoToken)
		if err != nil {
			return databaseConfig{}, err
		}

		return databaseConfig{driver: libSQLDriver, dsn: dsn}, nil
	}

	if tursoToken != "" {
		return databaseConfig{}, fmt.Errorf("%s is set but %s is empty", tursoAuthTokenEnv, tursoDatabaseURLEnv)
	}

	dbPath := strings.TrimSpace(os.Getenv(databasePathEnv))
	if dbPath == "" {
		dbPath = defaultDatabasePath
	}

	return databaseConfig{driver: sqliteDriver, dsn: dbPath}, nil
}

func buildTursoDSN(databaseURL, authToken string) (string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("invalid %s: %w", tursoDatabaseURLEnv, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%s must include a scheme and host", tursoDatabaseURLEnv)
	}

	query := parsed.Query()
	query.Set("authToken", authToken)
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}

func openConfiguredDatabase() (*sql.DB, error) {
	cfg, err := loadDatabaseConfig()
	if err != nil {
		return nil, err
	}

	return sql.Open(cfg.driver, cfg.dsn)
}

func main() {
	godotenv.Load()

	sqlDB, err := openConfiguredDatabase()
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}
	defer sqlDB.Close()

	// Applying schema.sql on every boot only worked while every statement was
	// idempotent. The migration runner records what it has applied, so an
	// ALTER TABLE runs once instead of failing every restart after the first.
	if err := db.Migrate(context.Background(), sqlDB); err != nil {
		log.Fatal("Failed to run migrations: ", err)
	}

	queries := db.New(sqlDB)

	// This used to fall back to a hardcoded string. That string is in a public
	// repository, and it signs both the session cookie and now the API's access
	// tokens — anyone holding it can mint a session for any user id. Refusing to
	// start is the only safe behaviour.
	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		log.Fatal("SESSION_SECRET is not set. Generate one with " +
			"`openssl rand -base64 32` and set it, e.g. " +
			"`heroku config:set SESSION_SECRET=...`")
	}
	store := sessions.NewCookieStore([]byte(sessionSecret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 365, // 1 year
		HttpOnly: true,
		// Inferred from the OAuth redirect URL rather than configured
		// separately, so a TLS deployment gets a Secure cookie automatically
		// while local development over http keeps working.
		Secure:   strings.HasPrefix(os.Getenv("GOOGLE_REDIRECT_URL"), "https://"),
		SameSite: http.SameSiteLaxMode,
	}

	templates, err := template.ParseFS(templatesFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		log.Fatal("Failed to parse templates:", err)
	}

	signer, err := auth.NewSigner(sessionSecret)
	if err != nil {
		log.Fatal("Failed to build token signer: ", err)
	}
	googleVerifier := auth.NewGoogleVerifier(os.Getenv("GOOGLE_CLIENT_ID"))

	h := handlers.New(queries, templates)
	authHandler := handlers.NewAuthHandler(queries, store)
	apiHandler := handlers.NewAPIHandler(queries, signer, googleVerifier)
	sessionMiddleware := middleware.NewSessionMiddleware(store, queries)
	apiAuth := middleware.NewAPIAuth(signer)

	r := chi.NewRouter()
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Compress(5))

	r.Handle("/static/*", http.FileServer(http.FS(staticFS)))

	r.Group(func(r chi.Router) {
		r.Use(sessionMiddleware.Handler)

		r.Get("/", h.Index)
		r.Get("/month", h.GetMonth)
		r.Get("/missed", h.GetMissedDays)
		r.Post("/toggle/{day}", h.ToggleDay)

		r.Get("/auth/google", authHandler.GoogleLogin)
		r.Get("/auth/google/callback", authHandler.GoogleCallback)
		r.Get("/logout", authHandler.Logout)
	})

	// The JSON API for the Android app. Deliberately outside the session group:
	// that middleware inserts an anonymous users row for every request without a
	// session cookie, which for an API would mean a junk row per unauthenticated
	// call.
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/google", apiHandler.SignInWithGoogle)
		r.Post("/auth/refresh", apiHandler.Refresh)

		// The plan is the same for everyone, so it needs no authentication.
		r.Get("/plan", apiHandler.GetPlan)

		r.Group(func(r chi.Router) {
			r.Use(apiAuth.Handler)
			r.Get("/progress", apiHandler.GetProgress)
			r.Post("/progress", apiHandler.PushProgress)
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8493"
	}

	log.Printf("Server starting on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal("Server failed:", err)
	}
}
