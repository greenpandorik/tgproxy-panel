-- name: CreateSession :exec
INSERT INTO sessions (id, admin_user_id, expires_at, ip, user_agent) VALUES ($1, $2, $3, $4, $5);

-- name: GetSession :one
SELECT s.*, u.username, u.role FROM sessions s JOIN admin_users u ON u.id = s.admin_user_id
WHERE s.id = $1 AND s.expires_at > now();

-- name: TouchSession :exec
UPDATE sessions SET expires_at = $2 WHERE id = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = $1;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE admin_user_id = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= now();
