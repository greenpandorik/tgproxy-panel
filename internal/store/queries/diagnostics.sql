-- name: InsertNodeDiagnostics :one
INSERT INTO node_diagnostics (node_id, started_at, finished_at, overall_status, trigger, checks)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListNodeDiagnostics :many
SELECT * FROM node_diagnostics WHERE node_id = $1 ORDER BY started_at DESC LIMIT $2;
