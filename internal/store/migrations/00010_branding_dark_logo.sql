-- +goose Up
ALTER TABLE branding_profiles ADD COLUMN logo_dark_path text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE branding_profiles DROP COLUMN logo_dark_path;
