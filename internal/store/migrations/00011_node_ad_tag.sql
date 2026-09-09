-- +goose Up
ALTER TABLE nodes ADD COLUMN ad_tag text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE nodes DROP COLUMN ad_tag;
