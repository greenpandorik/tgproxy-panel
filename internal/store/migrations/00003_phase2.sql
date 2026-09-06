-- +goose Up
-- last_check holds the most recent nodecheck.Report (JSON), or NULL when the
-- prerequisite check has never been run for this node.
ALTER TABLE nodes ADD COLUMN last_check jsonb;

-- +goose Down
ALTER TABLE nodes DROP COLUMN last_check;
