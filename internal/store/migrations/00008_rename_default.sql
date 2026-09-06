-- +goose Up
-- The product is "TGProxy Panel" (github.com/greenpandorik/tgproxy-panel). The
-- template-era default name and the generic blue/green colour pair go with it:
-- the column defaults move to the brand values (docs/design/brand.md), and rows an
-- operator never touched - still exactly the old defaults - follow them. Anything
-- customised is left alone.
ALTER TABLE branding_profiles ALTER COLUMN panel_name SET DEFAULT 'TGProxy Panel';
ALTER TABLE branding_profiles ALTER COLUMN primary_color SET DEFAULT '#e23c92';
ALTER TABLE branding_profiles ALTER COLUMN accent_color SET DEFAULT '#12a198';
UPDATE branding_profiles SET panel_name = 'TGProxy Panel' WHERE panel_name = 'WEB Proxy Panel';
UPDATE branding_profiles SET primary_color = '#e23c92', accent_color = '#12a198'
  WHERE primary_color = '#3b82f6' AND accent_color = '#22c55e';

-- +goose Down
ALTER TABLE branding_profiles ALTER COLUMN panel_name SET DEFAULT 'WEB Proxy Panel';
ALTER TABLE branding_profiles ALTER COLUMN primary_color SET DEFAULT '#3b82f6';
ALTER TABLE branding_profiles ALTER COLUMN accent_color SET DEFAULT '#22c55e';
UPDATE branding_profiles SET panel_name = 'WEB Proxy Panel' WHERE panel_name = 'TGProxy Panel';
UPDATE branding_profiles SET primary_color = '#3b82f6', accent_color = '#22c55e'
  WHERE primary_color = '#e23c92' AND accent_color = '#12a198';
