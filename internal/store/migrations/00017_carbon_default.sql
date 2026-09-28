-- +goose Up
ALTER TABLE branding_profiles ALTER COLUMN primary_color SET DEFAULT '#c4ed79';
ALTER TABLE branding_profiles ALTER COLUMN accent_color SET DEFAULT '#c0a8ed';
UPDATE branding_profiles SET primary_color = '#c4ed79', accent_color = '#c0a8ed'
  WHERE lower(primary_color) = '#e23c92' AND lower(accent_color) = '#12a198';

-- +goose Down
ALTER TABLE branding_profiles ALTER COLUMN primary_color SET DEFAULT '#e23c92';
ALTER TABLE branding_profiles ALTER COLUMN accent_color SET DEFAULT '#12a198';
UPDATE branding_profiles SET primary_color = '#e23c92', accent_color = '#12a198'
  WHERE primary_color = '#c4ed79' AND accent_color = '#c0a8ed';
