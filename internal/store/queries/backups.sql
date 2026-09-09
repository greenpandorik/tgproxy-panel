
-- name: InsertBackup :one
INSERT INTO backups (path, size, kind) VALUES ($1, $2, $3) RETURNING *;

-- name: ListBackups :many
SELECT * FROM backups ORDER BY created_at DESC;

-- name: GetBackup :one
SELECT * FROM backups WHERE id = $1;

-- name: DeleteBackup :execrows
DELETE FROM backups WHERE id = $1;

-- CountBackupsSince answers "has the nightly dump already run today?" without
-- pulling the whole list into the worker.
-- name: CountBackupsSince :one
SELECT count(*) FROM backups WHERE kind = $1 AND created_at >= $2;
