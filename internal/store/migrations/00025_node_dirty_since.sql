-- +goose Up
ALTER TABLE nodes ADD COLUMN dirty_since timestamptz;

-- +goose Down
ALTER TABLE nodes DROP COLUMN dirty_since;
