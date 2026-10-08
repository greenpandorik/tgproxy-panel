-- name: LockAPITokenOwner :one
SELECT id FROM admin_users WHERE id = $1 FOR UPDATE;

-- name: CountActiveAPITokens :one
SELECT count(*) FROM api_tokens WHERE admin_user_id = $1 AND revoked_at IS NULL AND expires_at > now();

-- name: CreateAPIToken :one
INSERT INTO api_tokens (admin_user_id, name, prefix, token_hash, scopes, expires_at)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: ListAPITokens :many
SELECT id, name, prefix, scopes, created_at, expires_at, last_used_at, revoked_at
FROM api_tokens WHERE admin_user_id = $1 ORDER BY created_at DESC, id DESC;

-- name: GetActiveAPIToken :one
SELECT t.id, t.admin_user_id, t.scopes, a.username, a.role FROM api_tokens t
JOIN admin_users a ON a.id = t.admin_user_id
WHERE t.token_hash = $1 AND t.revoked_at IS NULL AND t.expires_at > now();

-- name: TouchAPIToken :exec
UPDATE api_tokens SET last_used_at = now() WHERE id = $1
AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');

-- name: RevokeAPIToken :execrows
UPDATE api_tokens SET revoked_at = COALESCE(revoked_at, now()) WHERE id = $1 AND admin_user_id = $2;

-- name: RevokeAdminAPITokens :exec
UPDATE api_tokens SET revoked_at = COALESCE(revoked_at, now()) WHERE admin_user_id = $1;
