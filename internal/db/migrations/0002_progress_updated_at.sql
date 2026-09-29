-- Mobile clients sync progress with last-write-wins, which needs a timestamp on
-- every change. completed_at cannot serve: it is cleared when a day is
-- unticked, so "unticked on the phone at 9" could not be ordered against
-- "ticked on the web at 8".
--
-- Stored as epoch milliseconds rather than DATETIME on purpose. SQLite keeps
-- DATETIME as text, so `a > b` compares strings; two rows written with
-- different timezone offsets would then order wrongly, and ordering is the
-- whole point of this column. Integers also match the clock the Android client
-- stamps its pending rows with.
ALTER TABLE reading_progress ADD COLUMN updated_at INTEGER;

-- NOTE: once this has run against production, migrations are append-only.
-- Editing an applied file changes nothing, because its version is recorded.

-- Existing rows get the best timestamp available. A completed row keeps the
-- moment it was completed; anything else is stamped now, which is honest —
-- the server has no better claim about when it last changed.
--
-- completed_at is stored as text by whichever driver wrote it, and the formats
-- differ: modernc's sqlite writes Go's own layout,
-- "2026-03-04 05:06:07.000000000 +0000 UTC", which SQLite cannot parse at all —
-- strftime returns NULL on it. Its first 19 characters are a valid SQLite
-- datetime, so that is the fallback. A driver that writes RFC3339 parses on the
-- first branch. Anything unparseable is stamped now rather than left NULL,
-- because a NULL here would keep the row invisible to every delta pull forever.
UPDATE reading_progress
   SET updated_at = COALESCE(
           CAST(strftime('%s', completed_at) AS INTEGER),
           CAST(strftime('%s', substr(completed_at, 1, 19)) AS INTEGER),
           CAST(strftime('%s', 'now') AS INTEGER)
       ) * 1000
 WHERE updated_at IS NULL;

-- Delta pulls ask for one user's rows changed since a cursor.
CREATE INDEX IF NOT EXISTS idx_progress_user_updated
    ON reading_progress(user_id, updated_at);
