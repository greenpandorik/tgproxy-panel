-- +goose Up
ALTER TABLE alerts ADD COLUMN read_at timestamptz;

-- +goose Down
ALTER TABLE alerts DROP COLUMN read_at;
