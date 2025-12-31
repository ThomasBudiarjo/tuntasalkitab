# Bible Reading Tracker

A simple web application to track your daily Bible reading progress through the year.

## Features

- **365-Day Reading Plan**: Sequential reading from Genesis to Revelation
- **Monthly View**: Clean checklist interface organized by month
- **Progress Tracking**: Visual progress bar showing yearly completion
- **Strikethrough**: Completed readings are struck through for clarity
- **Optional Google Sign-in**: Sync your progress across devices
- **SQLite Database**: Lightweight, file-based storage
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

3. **Run the application:**
   ```bash
   go run main.go
   ```

4. **Open in browser:**
   ```
   http://localhost:8080
   ```

## Google OAuth Setup (Optional)

To enable Google Sign-in:

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select an existing one
3. Enable the Google+ API
4. Go to Credentials → Create Credentials → OAuth Client ID
5. Select "Web application"
6. Add `http://localhost:8080/auth/google/callback` to Authorized redirect URIs
7. Copy the Client ID and Client Secret to your `.env` file

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

