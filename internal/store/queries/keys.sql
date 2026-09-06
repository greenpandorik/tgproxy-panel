-- name: CreateKey :one
INSERT INTO access_keys (label, type, owner_label, secret_enc, carrier_mode, limits, expires_at, note, created_by, telemt_limits)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE(sqlc.narg('telemt_limits')::jsonb, '{}'::jsonb)) RETURNING *;

-- name: GetKey :one
SELECT * FROM access_keys WHERE id = $1;

-- name: ListKeys :many
SELECT k.* FROM access_keys k
WHERE (sqlc.narg('type')::key_type IS NULL OR k.type = sqlc.narg('type'))
  AND (sqlc.narg('status')::key_status IS NULL OR k.status = sqlc.narg('status'))
  AND (sqlc.narg('node_id')::uuid IS NULL OR EXISTS (SELECT 1 FROM key_bindings b WHERE b.access_key_id = k.id AND b.node_id = sqlc.narg('node_id')))
  AND (sqlc.narg('q')::text IS NULL OR k.label ILIKE '%' || sqlc.narg('q') || '%' OR k.owner_label ILIKE '%' || sqlc.narg('q') || '%')
ORDER BY k.created_at DESC LIMIT $1 OFFSET $2;

-- name: CountKeys :one
SELECT count(*) FROM access_keys k
WHERE (sqlc.narg('type')::key_type IS NULL OR k.type = sqlc.narg('type'))
  AND (sqlc.narg('status')::key_status IS NULL OR k.status = sqlc.narg('status'))
  AND (sqlc.narg('node_id')::uuid IS NULL OR EXISTS (SELECT 1 FROM key_bindings b WHERE b.access_key_id = k.id AND b.node_id = sqlc.narg('node_id')))
  AND (sqlc.narg('q')::text IS NULL OR k.label ILIKE '%' || sqlc.narg('q') || '%' OR k.owner_label ILIKE '%' || sqlc.narg('q') || '%');

-- name: UpdateKey :one
UPDATE access_keys SET label = $2, owner_label = $3, note = $4, expires_at = $5, carrier_mode = $6, limits = $7,
  telemt_limits = COALESCE(sqlc.narg('telemt_limits')::jsonb, telemt_limits)
WHERE id = $1 RETURNING *;

-- name: SetKeyStatus :exec
UPDATE access_keys SET status = $2::key_status, revoked_at = CASE WHEN $2::key_status = 'revoked' THEN now() ELSE revoked_at END WHERE id = $1;

-- name: SetKeySecret :exec
UPDATE access_keys SET secret_enc = $2, status = 'pending', revoked_at = NULL WHERE id = $1;

-- name: SetKeyExpiry :exec
UPDATE access_keys SET expires_at = $2 WHERE id = $1;

-- name: DeleteKey :exec
DELETE FROM access_keys WHERE id = $1;

-- name: ListExpiredActiveKeys :many
SELECT * FROM access_keys WHERE status IN ('pending','active') AND expires_at IS NOT NULL AND expires_at <= now();

-- name: CountKeysByStatus :many
SELECT status, count(*) AS n FROM access_keys GROUP BY status;

-- name: CreateBinding :exec
INSERT INTO key_bindings (access_key_id, node_id, profile_id) VALUES ($1, $2, $3);

-- name: DeleteBinding :exec
DELETE FROM key_bindings WHERE access_key_id = $1 AND node_id = $2;

-- name: ListKeyBindings :many
SELECT b.*, n.name AS node_name, n.hostname, n.engine, n.tls_domain, n.classic_port, p.sync_state FROM key_bindings b
JOIN nodes n ON n.id = b.node_id JOIN profiles p ON p.id = b.profile_id
WHERE b.access_key_id = $1 ORDER BY n.name;

-- name: ListNodeKeyBindings :many
SELECT b.*, k.status AS key_status FROM key_bindings b JOIN access_keys k ON k.id = b.access_key_id WHERE b.node_id = $1;

-- name: ActivatePendingKeysForNode :exec
UPDATE access_keys k SET status = 'active' WHERE k.status = 'pending'
  AND EXISTS (SELECT 1 FROM profiles p WHERE p.access_key_id = k.id)
  AND NOT EXISTS (SELECT 1 FROM profiles p WHERE p.access_key_id = k.id AND p.sync_state <> 'synced');

-- ListBindingsForKeys is ListKeyBindings for a whole page of keys at once, so
-- the list endpoint no longer issues one binding query per key.
-- name: ListBindingsForKeys :many
SELECT b.*, n.name AS node_name, n.hostname, n.engine, n.tls_domain, n.classic_port, p.sync_state FROM key_bindings b
JOIN nodes n ON n.id = b.node_id JOIN profiles p ON p.id = b.profile_id
WHERE b.access_key_id = ANY(sqlc.arg('key_ids')::uuid[]) ORDER BY b.access_key_id, n.name;

-- name: CreateSubscriptionToken :one
INSERT INTO subscription_tokens (token_hash, access_key_id) VALUES ($1, $2) RETURNING *;

-- name: GetSubscriptionByHash :one
SELECT * FROM subscription_tokens WHERE token_hash = $1;

-- name: RevokeSubscriptionTokensForKey :exec
UPDATE subscription_tokens SET revoked_at = now() WHERE access_key_id = $1 AND revoked_at IS NULL;

-- name: GetKeySubscription :one
SELECT * FROM subscription_tokens WHERE access_key_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC LIMIT 1;

-- ListActiveSubscriptionAccessKeyIDs is GetKeySubscription for a whole page of keys at
-- once, so the list endpoint can flag subscription_active without one query per row.
-- name: ListActiveSubscriptionAccessKeyIDs :many
SELECT DISTINCT access_key_id FROM subscription_tokens
WHERE access_key_id = ANY(sqlc.arg('key_ids')::uuid[]) AND revoked_at IS NULL;
