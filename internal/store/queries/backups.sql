-- backups.path holds the dump's file name, not an absolute path: the directory
-- is DATA_DIR/backups, which moves with the deployment (a bind mount today, a
-- different volume tomorrow), and every consumer resolves the name against the
-- runner's Dir anyway - which is also what keeps a row from ever naming a file
-- outside it.

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
