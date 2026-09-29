-- Server cursors are epoch-millisecond-like numbers, but wall-clock
-- milliseconds alone are not safe as a sync cursor: two requests can commit in
-- the same millisecond. This single-row clock preserves the public cursor shape
-- while making every issued server cursor strictly increasing.
CREATE TABLE IF NOT EXISTS sync_clock (
    id      INTEGER PRIMARY KEY CHECK (id = 1),
    last_ms INTEGER NOT NULL
);

INSERT OR IGNORE INTO sync_clock (id, last_ms)
VALUES (
    1,
    MAX(
        COALESCE((SELECT MAX(changed_at) FROM reading_progress), 0),
        CAST(strftime('%s', 'now') AS INTEGER) * 1000
    )
);
