package db

import (
	"context"
	"fmt"
)

const nextSyncCursorSQL = `
UPDATE sync_clock
   SET last_ms = CASE
           WHEN last_ms >= ? THEN last_ms + 1
           ELSE ?
       END
 WHERE id = 1
RETURNING last_ms`

// NextSyncCursor returns a server-side cursor that is strictly greater than any
// cursor this database has already issued, while staying epoch-millisecond-like
// for the Android API contract.
func (q *Queries) NextSyncCursor(ctx context.Context, nowMs int64) (int64, error) {
	if nowMs < 0 {
		nowMs = 0
	}

	var cursor int64
	var err error
	for attempt := 0; attempt < busyRetryAttempts; attempt++ {
		err = q.db.QueryRowContext(ctx, nextSyncCursorSQL, nowMs, nowMs).Scan(&cursor)
		if err == nil {
			return cursor, nil
		}
		if !isBusy(err) {
			return 0, fmt.Errorf("advance sync cursor: %w", err)
		}
		if waitErr := waitBeforeRetry(ctx, attempt); waitErr != nil {
			return 0, waitErr
		}
	}
	return 0, fmt.Errorf("advance sync cursor: %w", err)
}
