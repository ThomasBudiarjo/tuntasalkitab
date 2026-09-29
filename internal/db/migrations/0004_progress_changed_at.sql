-- Delta pulls need a server-side cursor. updated_at is intentionally supplied
-- by the writing device so last-write-wins works across offline clients, but a
-- device clock can be behind the server cursor another client already stored.
ALTER TABLE reading_progress ADD COLUMN changed_at INTEGER;

-- Existing rows changed when the server first learned about them, but before
-- this migration we did not record that moment. updated_at is the best
-- available ordering signal for backfill; current and future writes set
-- changed_at from the server clock.
UPDATE reading_progress
   SET changed_at = COALESCE(updated_at, CAST(strftime('%s', 'now') AS INTEGER) * 1000)
 WHERE changed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_progress_user_changed
    ON reading_progress(user_id, changed_at);
