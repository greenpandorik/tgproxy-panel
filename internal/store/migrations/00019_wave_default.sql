-- +goose Up
ALTER TABLE branding_profiles ALTER COLUMN primary_color SET DEFAULT '#3fc0d6';
ALTER TABLE branding_profiles ALTER COLUMN accent_color SET DEFAULT '#20c997';
UPDATE branding_profiles SET primary_color = '#3fc0d6', accent_color = '#20c997'
  WHERE lower(primary_color) = '#c4ed79' AND lower(accent_color) = '#c0a8ed';

-- +goose Down
ALTER TABLE branding_profiles ALTER COLUMN primary_color SET DEFAULT '#c4ed79';
ALTER TABLE branding_profiles ALTER COLUMN accent_color SET DEFAULT '#c0a8ed';
UPDATE branding_profiles SET primary_color = '#c4ed79', accent_color = '#c0a8ed'
  WHERE primary_color = '#3fc0d6' AND accent_color = '#20c997';
