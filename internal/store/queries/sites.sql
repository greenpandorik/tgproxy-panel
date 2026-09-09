-- name: CreateSiteTemplate :one
INSERT INTO site_templates (name, html, assets, is_preset) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetSiteTemplate :one
SELECT * FROM site_templates WHERE id = $1;

-- name: GetPresetByName :one
SELECT * FROM site_templates WHERE is_preset = true AND name = $1;

-- name: ListSiteTemplates :many
SELECT id, name, is_preset, created_at, updated_at FROM site_templates ORDER BY is_preset DESC, name;

-- name: UpdateSiteTemplate :one
UPDATE site_templates SET name = $2, html = $3, assets = $4, updated_at = now() WHERE id = $1 RETURNING *;

-- name: DeleteSiteTemplate :exec
DELETE FROM site_templates WHERE id = $1 AND is_preset = false;

-- DeleteRetiredPresets removes built-in templates the panel no longer ships. A node that had
-- one keeps its deployed bundle: node_sites.template_id is ON DELETE SET NULL and the bundle
-- is the node's own copy. Custom templates are never touched.
-- name: DeleteRetiredPresets :execrows
DELETE FROM site_templates WHERE is_preset = true AND name <> ALL($1::text[]);

-- name: UpsertNodeSite :exec
INSERT INTO node_sites (node_id, template_id, bundle, bundle_hash) VALUES ($1, $2, $3, $4)
ON CONFLICT (node_id) DO UPDATE SET template_id = EXCLUDED.template_id, bundle = EXCLUDED.bundle, bundle_hash = EXCLUDED.bundle_hash, updated_at = now();
