package db

import (
	"context"
	"strings"
	"time"
)

const busyRetryAttempts = 10

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "SQLITE_BUSY") ||
		strings.Contains(message, "database is locked")
}

func waitBeforeRetry(ctx context.Context, attempt int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(attempt+1) * 10 * time.Millisecond):
		return nil
	}
}
