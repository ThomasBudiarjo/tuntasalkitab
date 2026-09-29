-- name: CreateUser :one
INSERT INTO users (google_id, email, name)
VALUES (?, ?, ?)
RETURNING *;

-- name: CreateAnonymousUser :one
INSERT INTO users (google_id, email, name)
VALUES (NULL, NULL, NULL)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByGoogleID :one
SELECT * FROM users WHERE google_id = ?;

-- name: UpdateUserGoogleID :exec
UPDATE users SET google_id = ?, email = ?, name = ? WHERE id = ?;

-- name: GetProgress :many
SELECT * FROM reading_progress WHERE user_id = ?;

-- name: GetProgressByDayRange :many
SELECT * FROM reading_progress
WHERE user_id = ? AND day_of_year >= ? AND day_of_year <= ?;

-- name: GetProgressByDay :one
SELECT * FROM reading_progress WHERE user_id = ? AND day_of_year = ?;

-- Rows changed since a cursor, for the mobile client's delta pull.
-- COALESCE guards rows written before updated_at existed, which would otherwise
-- compare as NULL and silently never sync.
-- name: GetProgressSince :many
SELECT * FROM reading_progress
WHERE user_id = ?
  AND COALESCE(changed_at, 0) > ?
  AND COALESCE(changed_at, 0) <= ?
ORDER BY changed_at;

-- name: UpsertProgress :exec
INSERT INTO reading_progress (user_id, day_of_year, completed, completed_at, updated_at, changed_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id, day_of_year) DO UPDATE SET
    completed = excluded.completed,
    completed_at = excluded.completed_at,
    updated_at = excluded.updated_at,
    changed_at = excluded.changed_at;

-- Last-write-wins upsert: the incoming row only lands if it is strictly newer
-- than what is stored. A tie leaves the server's row alone, which makes the
-- server the tiebreaker and keeps a retried push idempotent.
-- name: UpsertProgressIfNewer :exec
INSERT INTO reading_progress (user_id, day_of_year, completed, completed_at, updated_at, changed_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id, day_of_year) DO UPDATE SET
    completed = excluded.completed,
    completed_at = excluded.completed_at,
    updated_at = excluded.updated_at,
    changed_at = excluded.changed_at
WHERE excluded.updated_at > COALESCE(reading_progress.updated_at, 0);

-- name: CountCompletedDays :one
SELECT COUNT(*) FROM reading_progress WHERE user_id = ? AND completed = TRUE;

-- name: DeleteProgressForUser :exec
DELETE FROM reading_progress WHERE user_id = ?;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;

-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (token_hash, user_id, expires_at)
VALUES (?, ?, ?);

-- name: GetRefreshToken :one
SELECT token_hash, user_id, created_at, expires_at
FROM refresh_tokens WHERE token_hash = ?;

-- name: ConsumeRefreshToken :one
DELETE FROM refresh_tokens
WHERE token_hash = ?
RETURNING token_hash, user_id, created_at, expires_at;

-- name: DeleteRefreshToken :exec
DELETE FROM refresh_tokens WHERE token_hash = ?;

-- name: DeleteRefreshTokensForUser :exec
DELETE FROM refresh_tokens WHERE user_id = ?;

-- name: DeleteExpiredRefreshTokens :exec
DELETE FROM refresh_tokens WHERE expires_at < CURRENT_TIMESTAMP;
