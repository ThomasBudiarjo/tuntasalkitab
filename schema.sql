-- Users table (for OAuth, nullable for anonymous)
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    google_id TEXT UNIQUE,
    email TEXT,
    name TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Reading progress
CREATE TABLE IF NOT EXISTS reading_progress (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    day_of_year INTEGER NOT NULL,  -- 1-365
    completed BOOLEAN DEFAULT FALSE,
    completed_at DATETIME,
    -- Epoch milliseconds. See migrations/0002 for why this is not a DATETIME.
    updated_at INTEGER,
    -- Server-side epoch milliseconds used as the delta sync cursor.
    changed_at INTEGER,
    FOREIGN KEY (user_id) REFERENCES users(id),
    UNIQUE(user_id, day_of_year)
);

-- Index for faster lookups
CREATE INDEX IF NOT EXISTS idx_progress_user ON reading_progress(user_id);
CREATE INDEX IF NOT EXISTS idx_progress_day ON reading_progress(day_of_year);


-- Refresh tokens for the Android client. Only the SHA-256 of the token is
-- stored, so a database leak does not hand out working sessions.
CREATE TABLE IF NOT EXISTS refresh_tokens (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_progress_user_updated ON reading_progress(user_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_progress_user_changed ON reading_progress(user_id, changed_at);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
