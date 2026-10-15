-- +goose Up
ALTER TABLE nodes ADD COLUMN telemt_web_capacity integer
  CHECK (telemt_web_capacity IN (128, 256, 512, 1024));

-- +goose Down
ALTER TABLE nodes DROP COLUMN telemt_web_capacity;
