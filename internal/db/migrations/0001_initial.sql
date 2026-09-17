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
    FOREIGN KEY (user_id) REFERENCES users(id),
    UNIQUE(user_id, day_of_year)
);

-- Index for faster lookups
CREATE INDEX IF NOT EXISTS idx_progress_user ON reading_progress(user_id);
CREATE INDEX IF NOT EXISTS idx_progress_day ON reading_progress(day_of_year);

