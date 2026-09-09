-- name: InsertAudit :exec
INSERT INTO audit_log (admin_user_id, action, target_type, target_id, meta, ip) VALUES ($1, $2, $3, $4, $5, $6);

-- The id tiebreaker is load-bearing, not cosmetic: audit_log.id is a bigserial and entries
-- written inside one transaction share a created_at to the microsecond, so an unordered tie
-- lets Postgres return them in a different order per page and a row can appear on two pages
-- or on none.
-- name: ListAudit :many
SELECT a.*, u.username FROM audit_log a LEFT JOIN admin_users u ON u.id = a.admin_user_id
ORDER BY a.created_at DESC, a.id DESC LIMIT $1 OFFSET $2;

-- name: CountAudit :one
SELECT count(*) FROM audit_log;

-- name: ListAuditFiltered :many
SELECT a.*, u.username FROM audit_log a LEFT JOIN admin_users u ON u.id = a.admin_user_id
WHERE (sqlc.narg('action')::text IS NULL OR a.action LIKE sqlc.narg('action') || '%' ESCAPE '\')
  AND (sqlc.narg('username')::text IS NULL OR u.username = sqlc.narg('username'))
  AND (sqlc.narg('from_at')::timestamptz IS NULL OR a.created_at >= sqlc.narg('from_at'))
  AND (sqlc.narg('to_at')::timestamptz IS NULL OR a.created_at <= sqlc.narg('to_at'))
ORDER BY a.created_at DESC, a.id DESC LIMIT $1 OFFSET $2;

-- name: CountAuditFiltered :one
SELECT count(*) FROM audit_log a LEFT JOIN admin_users u ON u.id = a.admin_user_id
WHERE (sqlc.narg('action')::text IS NULL OR a.action LIKE sqlc.narg('action') || '%' ESCAPE '\')
  AND (sqlc.narg('username')::text IS NULL OR u.username = sqlc.narg('username'))
  AND (sqlc.narg('from_at')::timestamptz IS NULL OR a.created_at >= sqlc.narg('from_at'))
  AND (sqlc.narg('to_at')::timestamptz IS NULL OR a.created_at <= sqlc.narg('to_at'));

-- name: DeleteOldAudit :execrows
DELETE FROM audit_log WHERE created_at < $1;
