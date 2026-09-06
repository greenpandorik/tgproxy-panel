-- +goose Up
-- The retention sweep deletes by age (`WHERE taken_at < $1`), which the existing
-- (access_key_id, taken_at DESC) index cannot serve: a composite index is only
-- usable for a predicate on its leading column, so the sweep was a sequential
-- scan of the largest table in the database, once a minute, forever. This index
-- is on taken_at alone, so the sweep's inner `SELECT id ... WHERE taken_at < $1
-- ORDER BY taken_at LIMIT n` is an index scan, and KeyTrafficLast30d's window
-- filter can use it too when the key set is wide.
CREATE INDEX key_stats_taken_at_idx ON key_stats_snapshots(taken_at);

-- +goose Down
DROP INDEX key_stats_taken_at_idx;
