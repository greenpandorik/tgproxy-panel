-- +goose Up
ALTER TABLE nodes ADD COLUMN telemt_web_applied_base_path text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE nodes DROP COLUMN telemt_web_applied_base_path;
