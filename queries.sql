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

-- name: UpsertProgress :exec
INSERT INTO reading_progress (user_id, day_of_year, completed, completed_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(user_id, day_of_year) DO UPDATE SET
    completed = excluded.completed,
    completed_at = excluded.completed_at;

-- name: CountCompletedDays :one
SELECT COUNT(*) FROM reading_progress WHERE user_id = ? AND completed = TRUE;

-- name: MergeUserProgress :exec
-- Merge progress from source user to target user with conflict resolution.
-- On conflict: prefer completed=true, keep latest completed_at.
INSERT INTO reading_progress (user_id, day_of_year, completed, completed_at)
SELECT ?, day_of_year, completed, completed_at
FROM reading_progress
WHERE user_id = ?
ON CONFLICT(user_id, day_of_year) DO UPDATE SET
    completed = CASE 
        WHEN excluded.completed = 1 THEN 1 
        ELSE reading_progress.completed 
    END,
    completed_at = CASE 
        WHEN excluded.completed = 1 AND (reading_progress.completed = 0 OR excluded.completed_at > reading_progress.completed_at)
        THEN excluded.completed_at
        ELSE reading_progress.completed_at
    END;

-- name: DeleteUserProgress :exec
DELETE FROM reading_progress WHERE user_id = ?;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;

