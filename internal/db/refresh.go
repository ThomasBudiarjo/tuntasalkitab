package db

import "context"

// CreateRefreshTokenWithRetry stores a newly issued refresh token, retrying
// transient SQLite/libSQL busy responses at the write-lock boundary.
func (q *Queries) CreateRefreshTokenWithRetry(ctx context.Context, arg CreateRefreshTokenParams) error {
	var err error
	for attempt := 0; attempt < busyRetryAttempts; attempt++ {
		err = q.CreateRefreshToken(ctx, arg)
		if err == nil || !isBusy(err) {
			return err
		}
		if waitErr := waitBeforeRetry(ctx, attempt); waitErr != nil {
			return waitErr
		}
	}
	return err
}

// ConsumeRefreshTokenWithRetry atomically spends a refresh token, retrying
// transient SQLite/libSQL busy responses at the lock boundary.
func (q *Queries) ConsumeRefreshTokenWithRetry(ctx context.Context, tokenHash string) (RefreshToken, error) {
	var token RefreshToken
	var err error
	for attempt := 0; attempt < busyRetryAttempts; attempt++ {
		token, err = q.ConsumeRefreshToken(ctx, tokenHash)
		if err == nil || !isBusy(err) {
			return token, err
		}
		if waitErr := waitBeforeRetry(ctx, attempt); waitErr != nil {
			return RefreshToken{}, waitErr
		}
	}
	return RefreshToken{}, err
}
