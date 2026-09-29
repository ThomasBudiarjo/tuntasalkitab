package db

import (
	"context"
	"database/sql"
	"fmt"
)

// MergeProgress folds one user's reading progress into another, keeping
// whichever side touched each day more recently.
//
// This is written by hand rather than as a sqlc query. The natural form,
// `INSERT INTO reading_progress ... SELECT ... ON CONFLICT ... DO UPDATE ...
// WHERE ...`, is generated wrong by sqlc v1.30.0: it truncates the statement
// mid-expression, and the resulting SQL fails at runtime with "incomplete
// input". Looping in Go instead also means the merge and the API's push share
// one conflict rule — UpsertProgressIfNewer — rather than having two
// implementations of last-write-wins that could drift.
//
// It replaces `UPDATE reading_progress SET user_id = ?`, which violated
// UNIQUE(user_id, day_of_year) whenever both users had touched the same day.
// That is the normal case when someone reads as a guest and then signs in, and
// the failure used to be swallowed, so their guest progress simply vanished.
func (q *Queries) MergeProgress(ctx context.Context, sourceUserID, targetUserID int64, changedAt int64) error {
	if sourceUserID == targetUserID {
		return nil
	}

	rows, err := q.GetProgress(ctx, sourceUserID)
	if err != nil {
		return fmt.Errorf("read source progress: %w", err)
	}

	for _, row := range rows {
		if err := q.UpsertProgressIfNewer(ctx, UpsertProgressIfNewerParams{
			UserID:      targetUserID,
			DayOfYear:   row.DayOfYear,
			Completed:   row.Completed,
			CompletedAt: row.CompletedAt,
			UpdatedAt:   row.UpdatedAt,
			ChangedAt:   sqlNullInt64(changedAt),
		}); err != nil {
			return fmt.Errorf("merge day %d: %w", row.DayOfYear, err)
		}
	}

	return nil
}

func sqlNullInt64(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: true}
}
