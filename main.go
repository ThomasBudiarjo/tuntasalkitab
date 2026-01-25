package main

import (
	"context"
	"database/sql"
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	appEnv := os.Getenv("APP_ENV")
	isDev := appEnv == "" || appEnv == "development"

	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		if isDev {
			sessionSecret = "bible-tracker-dev-secret-key"
		} else {
			log.Fatal("SESSION_SECRET environment variable is required in production")
		}
	}
	store := sessions.NewCookieStore([]byte(sessionSecret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 30, // 30 days
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !isDev,
	}

	templates, err := template.ParseFS(templatesFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		log.Fatal("Failed to parse templates:", err)
	}

	h := handlers.New(queries, templates)
	authHandler := handlers.NewAuthHandler(queries, store)
	sessionMiddleware := middleware.NewSessionMiddleware(store, queries)
	csrfMiddleware := middleware.NewCSRFMiddleware(store)

	r := chi.NewRouter()
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Compress(5))

	r.Handle("/static/*", http.FileServer(http.FS(staticFS)))

	r.Group(func(r chi.Router) {
		r.Use(sessionMiddleware.Handler)
		r.Use(csrfMiddleware.Handler)

		r.Get("/", h.Index)
		r.Get("/month", h.GetMonth)
		r.Post("/toggle/{day}", h.ToggleDay)

		r.Get("/auth/google", authHandler.GoogleLogin)
		r.Get("/auth/google/callback", authHandler.GoogleCallback)
		r.Post("/logout", authHandler.Logout)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("Server starting on http://localhost:%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server failed:", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}
	log.Println("Server exited gracefully")
}

