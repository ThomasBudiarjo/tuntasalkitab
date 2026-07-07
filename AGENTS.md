# AGENTS.md

## Cursor Cloud specific instructions

This is a single-service Go web app ("Tuntas Alkitab", a Bible reading tracker). Go 1.25 is auto-managed by the Go toolchain directive in `go.mod`.

### Services
- **Web server** (only service). Serves HTML (Go templates + HTMX) and a small JSON/HTMX API on `PORT` (default `8493`). Uses an embedded schema (`schema.sql`) applied on startup.

### Run / build / lint / test
- Run (dev): `go run main.go` — starts on `http://localhost:8493`. There is no hot-reload; restart the process after code changes.
- Build: `go build -o bible-tracker .`
- Lint / vet: `go vet ./...`
- Tests: none exist in the repo (`go test ./...` is a no-op that passes).

### Non-obvious notes
- **Database is local SQLite by default.** With `TURSO_DATABASE_URL`/`TURSO_AUTH_TOKEN` empty (the default in `.env.example`), it uses a local file at `DATABASE_PATH` (default `bible-tracker.db`). No external DB service is needed to run or test. The `.db` files are gitignored.
- Copy `.env.example` to `.env` before running if you want to customize config; the app also runs with defaults if `.env` is absent (`godotenv.Load()` is best-effort).
- Google OAuth and Turso are fully optional; leaving their env vars empty disables them and the core reading-tracker flow works without any credentials.
- The progress counter (`X/365`) is computed on page load, not reactively — after toggling reading days, refresh the page to see the count update. This is expected app behavior, not a bug in the environment.
- A prebuilt binary `tuntasalkitab` is committed at the repo root; ignore it for development and use `go run`/`go build` instead.
