package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// openTestDB returns an empty database backed by a file in the test's temp dir.
// A file rather than :memory: because database/sql pools connections, and each
// connection to :memory: gets its own private database.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

func TestMigrateCreatesSchema(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)

	if err := Migrate(ctx, sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{"users", "reading_progress", "refresh_tokens", "schema_migrations"} {
		var name string
		err := sqlDB.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name = ?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing after migrate: %v", table, err)
		}
	}

	var applied int
	if err := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&applied); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if applied < 3 {
		t.Errorf("expected at least 3 migrations recorded, got %d", applied)
	}
}

// The regression this whole migration runner exists for.
//
// The server used to apply schema.sql with a bare Exec on every boot and
// log.Fatal on error. That survives only while every statement is idempotent,
// so the first ALTER TABLE would have made every restart after the first one
// fail to start.
func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)

	if err := Migrate(ctx, sqlDB); err != nil {
		t.Fatalf("first migrate: %v", err)
	}

	var afterFirst int
	if err := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&afterFirst); err != nil {
		t.Fatalf("count: %v", err)
	}

	// A restart, and another for good measure.
	for i := range 2 {
		if err := Migrate(ctx, sqlDB); err != nil {
			t.Fatalf("migrate on restart %d: %v", i+2, err)
		}
	}

	var afterRestarts int
	if err := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&afterRestarts); err != nil {
		t.Fatalf("count: %v", err)
	}
	if afterFirst != afterRestarts {
		t.Errorf("migrations re-applied on restart: %d then %d", afterFirst, afterRestarts)
	}
}

// An existing deployment has rows written before updated_at existed. They must
// come out of the migration with a usable timestamp, or the client's delta pull
// would never see them.
func TestMigrateBackfillsUpdatedAt(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)

	// Stand up the pre-migration schema and seed it, as production looked
	// before this change.
	initial, err := migrationsFS.ReadFile("migrations/0001_initial.sql")
	if err != nil {
		t.Fatalf("read initial migration: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, string(initial)); err != nil {
		t.Fatalf("apply initial schema: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		"INSERT INTO users (id, google_id) VALUES (1, 'g1')"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	completedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if _, err := sqlDB.ExecContext(ctx,
		"INSERT INTO reading_progress (user_id, day_of_year, completed, completed_at) VALUES (1, 10, 1, ?)",
		completedAt,
	); err != nil {
		t.Fatalf("seed completed row: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		"INSERT INTO reading_progress (user_id, day_of_year, completed, completed_at) VALUES (1, 11, 0, NULL)",
	); err != nil {
		t.Fatalf("seed incomplete row: %v", err)
	}

	if err := Migrate(ctx, sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var completedUpdatedAt, uncompletedUpdatedAt sql.NullInt64
	if err := sqlDB.QueryRowContext(ctx,
		"SELECT updated_at FROM reading_progress WHERE day_of_year = 10").Scan(&completedUpdatedAt); err != nil {
		t.Fatalf("read day 10: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx,
		"SELECT updated_at FROM reading_progress WHERE day_of_year = 11").Scan(&uncompletedUpdatedAt); err != nil {
		t.Fatalf("read day 11: %v", err)
	}

	if !completedUpdatedAt.Valid || !uncompletedUpdatedAt.Valid {
		t.Fatalf("updated_at left NULL: day10=%v day11=%v", completedUpdatedAt, uncompletedUpdatedAt)
	}
	if got, want := completedUpdatedAt.Int64, completedAt.UnixMilli(); got != want {
		t.Errorf("completed row: updated_at = %d, want completed_at %d", got, want)
	}
	if uncompletedUpdatedAt.Int64 <= 0 {
		t.Errorf("uncompleted row got a nonsensical updated_at: %d", uncompletedUpdatedAt.Int64)
	}
}

// Merging a guest into an account is the case the old query could not do. It
// was `UPDATE reading_progress SET user_id = ?`, which trips
// UNIQUE(user_id, day_of_year) as soon as both sides have touched the same day
// — which is exactly what happens when someone reads on the phone as a guest
// and then signs in.
func TestMergeUserProgressWithCollidingDays(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	if err := Migrate(ctx, sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	q := New(sqlDB)

	guest, err := q.CreateAnonymousUser(ctx)
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}
	account, err := q.CreateUser(ctx, CreateUserParams{
		GoogleID: sql.NullString{String: "g-123", Valid: true},
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	set := func(userID int64, day int64, completed bool, updatedAt int64) {
		t.Helper()
		if err := q.UpsertProgress(ctx, UpsertProgressParams{
			UserID:    userID,
			DayOfYear: day,
			Completed: sql.NullBool{Bool: completed, Valid: true},
			UpdatedAt: sql.NullInt64{Int64: updatedAt, Valid: true},
		}); err != nil {
			t.Fatalf("upsert user %d day %d: %v", userID, day, err)
		}
	}

	// Day 1: only the guest has it.
	set(guest.ID, 1, true, 1000)
	// Day 2: both have it; the guest's is newer, so the guest should win.
	set(account.ID, 2, false, 1000)
	set(guest.ID, 2, true, 2000)
	// Day 3: both have it; the account's is newer, so the guest must not undo it.
	set(guest.ID, 3, false, 1000)
	set(account.ID, 3, true, 2000)

	if err := q.MergeProgress(ctx, guest.ID, account.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	rows, err := q.GetProgress(ctx, account.ID)
	if err != nil {
		t.Fatalf("read merged progress: %v", err)
	}

	got := make(map[int64]bool, len(rows))
	for _, row := range rows {
		got[row.DayOfYear] = row.Completed.Bool
	}
	want := map[int64]bool{1: true, 2: true, 3: true}
	if len(got) != len(want) {
		t.Fatalf("merged progress has %d days, want %d: %v", len(got), len(want), got)
	}
	for day, completed := range want {
		if got[day] != completed {
			t.Errorf("day %d: completed = %v, want %v", day, got[day], completed)
		}
	}

	// The guest's rows are removed separately, so the account keeps its own
	// copies and nothing is left pointing at a deleted user.
	if err := q.DeleteProgressForUser(ctx, guest.ID); err != nil {
		t.Fatalf("clear guest progress: %v", err)
	}
	if err := q.DeleteUser(ctx, guest.ID); err != nil {
		t.Fatalf("delete guest: %v", err)
	}
	remaining, err := q.GetProgress(ctx, guest.ID)
	if err != nil {
		t.Fatalf("read guest progress: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("guest still has %d rows after cleanup", len(remaining))
	}
}

func TestUpsertProgressIfNewerRejectsStaleWrites(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	if err := Migrate(ctx, sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	q := New(sqlDB)

	user, err := q.CreateAnonymousUser(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	push := func(completed bool, updatedAt int64) {
		t.Helper()
		if err := q.UpsertProgressIfNewer(ctx, UpsertProgressIfNewerParams{
			UserID:    user.ID,
			DayOfYear: 42,
			Completed: sql.NullBool{Bool: completed, Valid: true},
			UpdatedAt: sql.NullInt64{Int64: updatedAt, Valid: true},
		}); err != nil {
			t.Fatalf("push: %v", err)
		}
	}
	state := func() (bool, int64) {
		t.Helper()
		row, err := q.GetProgressByDay(ctx, GetProgressByDayParams{UserID: user.ID, DayOfYear: 42})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return row.Completed.Bool, row.UpdatedAt.Int64
	}

	push(true, 2000)
	if completed, at := state(); !completed || at != 2000 {
		t.Fatalf("first write: completed=%v at=%d", completed, at)
	}

	// An older change — a phone that was offline and has stale state — must not
	// overwrite a newer one.
	push(false, 1000)
	if completed, at := state(); !completed || at != 2000 {
		t.Errorf("stale write was applied: completed=%v at=%d", completed, at)
	}

	// A tie leaves the server's row alone, which makes a retried push a no-op
	// rather than a flip-flop.
	push(false, 2000)
	if completed, _ := state(); !completed {
		t.Errorf("equal timestamp overwrote the server's row")
	}

	push(false, 3000)
	if completed, at := state(); completed || at != 3000 {
		t.Errorf("newer write was not applied: completed=%v at=%d", completed, at)
	}
}
