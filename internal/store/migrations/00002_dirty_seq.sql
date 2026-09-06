-- +goose Up
-- dirty_seq is bumped every time a node is marked dirty. An apply records the value it
-- read alongside its profile snapshot and only clears `dirty` when the counter is still
-- unchanged, so a mutation that lands mid-apply can never be reported as synced.
ALTER TABLE nodes ADD COLUMN dirty_seq bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE nodes DROP COLUMN dirty_seq;
