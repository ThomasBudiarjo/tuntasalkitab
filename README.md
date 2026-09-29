# Bible Reading Tracker

A simple web application to track your daily Bible reading progress through the year.

## Features

- **365-Day Reading Plan**: Sequential reading from Genesis to Revelation
- **Monthly View**: Clean checklist interface organized by month
- **Progress Tracking**: Visual progress bar showing yearly completion
- **Strikethrough**: Completed readings are struck through for clarity
- **Optional Google Sign-in**: Sync your progress across devices
- **SQLite/libSQL Database**: Local SQLite by default, with optional Turso/libSQL configuration
- **HTMX**: Fast, dynamic updates without page reloads

## Quick Start

1. **Clone and navigate to the project:**
   ```bash
   cd bible-tracker
   ```

2. **Copy environment file:**
   ```bash
   cp .env.example .env
   ```

3. **Set a session secret:**
   ```bash
   openssl rand -base64 32
   ```
   Paste the generated value into `SESSION_SECRET` in `.env`. The server
   refuses to boot without a secret of at least 16 bytes.

4. **Run the application:**
   ```bash
   go run main.go
   ```

5. **Open in browser:**
   ```
   http://localhost:8493
   ```

## Turso Setup (Optional)

Local SQLite is used when the Turso variables are empty. To use Turso/libSQL instead, create a Turso database and auth token, then set both variables in `.env`:

```dotenv
TURSO_DATABASE_URL=libsql://your-database-your-org.turso.io
TURSO_AUTH_TOKEN=your-turso-auth-token
```

When `TURSO_DATABASE_URL` is set, the application ignores `DATABASE_PATH` and opens the database with the `libsql` driver.

## Google OAuth Setup

`GOOGLE_CLIENT_ID` is required in production for the Android app's
`POST /api/v1/auth/google` endpoint. The Android app passes the web client ID
as its server client ID, so this value must be the web OAuth client ID whose
audience Google puts in the ID token.

`GOOGLE_CLIENT_SECRET` and `GOOGLE_REDIRECT_URL` are also needed for the
existing website Google sign-in flow.

To enable Google Sign-in:

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select an existing one
3. Enable the Google+ API
4. Go to Credentials → Create Credentials → OAuth Client ID
5. Select "Web application"
6. Add `http://localhost:8493/auth/google/callback` to Authorized redirect URIs
7. Copy the Client ID and Client Secret to your `.env` file

## Mobile JSON API

The Android client syncs through `/api/v1`:

| Endpoint | Purpose |
|---|---|
| `POST /api/v1/auth/google` | Google ID token to access and refresh tokens |
| `POST /api/v1/auth/refresh` | Rotate refresh tokens; the presented token is spent |
| `GET /api/v1/progress?since=` | Pull rows changed after a server epoch-millis cursor |
| `POST /api/v1/progress` | Push progress batches with last-write-wins conflict handling |
| `GET /api/v1/plan` | Fetch the reading plan with an ETag |

Before production deploy, set `SESSION_SECRET` and `GOOGLE_CLIENT_ID`. Do not
commit Google client secrets or other credentials.

## Project Structure

```
bible-tracker/
├── main.go              # Application entry point
├── schema.sql           # Database schema
├── queries.sql          # SQL queries for sqlc
├── sqlc.yaml            # sqlc configuration
├── internal/
│   ├── db/              # Generated database code
│   ├── handlers/        # HTTP handlers
│   ├── middleware/      # Session middleware
│   └── reading/         # Bible reading plan
├── templates/           # HTML templates
├── static/              # CSS and static files
└── data/                # Reading plan data
```

## Tech Stack

- **Go 1.21+** - Backend
- **Chi** - HTTP router
- **SQLite** - Database
- **sqlc** - Type-safe SQL
- **HTMX** - Frontend interactivity
- **Gorilla Sessions** - Session management

## Development

### Regenerate database code

```bash
sqlc generate
```

### Build for production

```bash
go build -o bible-tracker .
```

## License

MIT