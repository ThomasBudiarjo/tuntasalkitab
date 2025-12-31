package main

import (
	"database/sql"
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"

	"bible-tracker/internal/db"
	"bible-tracker/internal/handlers"
	"bible-tracker/internal/middleware"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed templates/*.html templates/partials/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

//go:embed schema.sql
var schemaSQL string

func main() {
	godotenv.Load()

	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "bible-tracker.db"
	}

	sqlDB, err := sql.Open("sqlite3", dbPath)
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
		r.Post("/toggle/{day}", h.ToggleDay)

		r.Get("/auth/google", authHandler.GoogleLogin)
		r.Get("/auth/google/callback", authHandler.GoogleCallback)
		r.Get("/logout", authHandler.Logout)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal("Server failed:", err)
	}
}

