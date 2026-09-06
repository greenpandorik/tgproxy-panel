-- name: GetActiveBranding :one
SELECT * FROM branding_profiles WHERE is_active = true LIMIT 1;

-- name: GetBranding :one
SELECT * FROM branding_profiles WHERE id = $1;

-- name: ListBranding :many
SELECT * FROM branding_profiles ORDER BY is_active DESC, name;

-- name: CreateBranding :one
INSERT INTO branding_profiles (name, panel_name, logo_path, favicon_path, primary_color, accent_color, theme_default, login_bg_path, login_text, support_link, footer_text, custom_css)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING *;

-- name: UpdateBranding :one
UPDATE branding_profiles SET name = $2, panel_name = $3, primary_color = $4, accent_color = $5, theme_default = $6, login_text = $7, support_link = $8, footer_text = $9, custom_css = $10, updated_at = now()
WHERE id = $1 RETURNING *;

-- name: SetBrandingAsset :exec
UPDATE branding_profiles SET
  logo_path = CASE WHEN $2::text = 'logo' THEN $3 ELSE logo_path END,
  favicon_path = CASE WHEN $2::text = 'favicon' THEN $3 ELSE favicon_path END,
  login_bg_path = CASE WHEN $2::text = 'login_bg' THEN $3 ELSE login_bg_path END,
  updated_at = now() WHERE id = $1;

-- name: DeactivateBranding :exec
UPDATE branding_profiles SET is_active = false WHERE is_active;

-- name: ActivateBrandingOne :exec
UPDATE branding_profiles SET is_active = true WHERE id = $1;

-- DeleteBranding returns the affected row count so the handler can tell a real
-- delete from a no-op: the profile was activated between the pre-check and this
-- statement, and the AND is_active = false predicate silently matched nothing.
-- name: DeleteBranding :execrows
DELETE FROM branding_profiles WHERE id = $1 AND is_active = false;
