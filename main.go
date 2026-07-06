package main

import (
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"bible-tracker/internal/db"
	"bible-tracker/internal/handlers"
	"bible-tracker/internal/middleware"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

//go:embed templates/*.html templates/partials/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

//go:embed schema.sql
var schemaSQL string

const (
	databasePathEnv     = "DATABASE_PATH"
	tursoDatabaseURLEnv = "TURSO_DATABASE_URL"
	tursoAuthTokenEnv   = "TURSO_AUTH_TOKEN"

	defaultDatabasePath = "bible-tracker.db"
	sqliteDriver        = "sqlite3"
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

	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		log.Fatal("Failed to run migrations:", err)
	}

	queries := db.New(sqlDB)

	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		sessionSecret = "bible-tracker-secret-key-change-in-production"
	}
	store := sessions.NewCookieStore([]byte(sessionSecret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 365, // 1 year
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	templates, err := template.ParseFS(templatesFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		log.Fatal("Failed to parse templates:", err)
	}

	h := handlers.New(queries, templates)
	authHandler := handlers.NewAuthHandler(queries, store)
	sessionMiddleware := middleware.NewSessionMiddleware(store, queries)

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

	port := os.Getenv("PORT")
	if port == "" {
		port = "8493"
	}

	log.Printf("Server starting on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal("Server failed:", err)
	}
}
