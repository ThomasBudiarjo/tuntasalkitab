package db

import (
	"context"
	"fmt"
)

const upsertGoogleUserSQL = `
INSERT INTO users (google_id, email, name)
VALUES (?, ?, ?)
ON CONFLICT(google_id) DO UPDATE SET
    email = excluded.email,
    name = excluded.name
RETURNING id, google_id, email, name, created_at`

// UpsertGoogleUser returns the row for a Google subject, creating it on first
// sign-in and reusing it on retries or concurrent first sign-ins.
func (q *Queries) UpsertGoogleUser(ctx context.Context, arg CreateUserParams) (User, error) {
	var user User
	var err error
	for attempt := 0; attempt < busyRetryAttempts; attempt++ {
		row := q.db.QueryRowContext(ctx, upsertGoogleUserSQL, arg.GoogleID, arg.Email, arg.Name)
		err = row.Scan(
			&user.ID,
			&user.GoogleID,
			&user.Email,
			&user.Name,
			&user.CreatedAt,
		)
		if err == nil {
			return user, nil
		}
		if !isBusy(err) {
			return User{}, fmt.Errorf("upsert google user: %w", err)
		}
		if waitErr := waitBeforeRetry(ctx, attempt); waitErr != nil {
			return User{}, waitErr
		}
	}
	return User{}, fmt.Errorf("upsert google user: %w", err)
}
