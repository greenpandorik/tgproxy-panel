-- name: GetNodeBlocklist :one
SELECT * FROM node_blocklists WHERE node_id = $1;

-- name: SaveNodeBlocklist :one
INSERT INTO node_blocklists (node_id, entries, revision, updated_at)
VALUES ($1, $2, 1, now())
ON CONFLICT (node_id) DO UPDATE
  SET entries = EXCLUDED.entries, revision = node_blocklists.revision + 1, updated_at = now()
RETURNING *;

-- name: ListBlocklistsToSync :many
SELECT b.node_id, b.entries, b.revision, n.last_health
FROM node_blocklists b
JOIN nodes n ON n.id = b.node_id
WHERE n.status IN ('online', 'degraded');
