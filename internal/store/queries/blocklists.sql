-- name: GetNodeBlocklist :one
SELECT * FROM node_blocklists WHERE node_id = $1;

-- name: SaveNodeBlocklist :one
INSERT INTO node_blocklists (node_id, entries, revision, updated_at)
VALUES (sqlc.arg('node_id'), sqlc.arg('entries'), 1, now())
ON CONFLICT (node_id) DO UPDATE
  SET entries = EXCLUDED.entries, revision = node_blocklists.revision + 1, updated_at = now()
  WHERE sqlc.narg('expected_revision')::bigint IS NULL OR node_blocklists.revision = sqlc.narg('expected_revision')::bigint
RETURNING *;

-- name: ListBlocklistsToSync :many
SELECT n.id AS node_id,
       COALESCE(b.entries, '[]'::jsonb)::jsonb AS entries,
       COALESCE(b.revision, 0)::bigint AS revision,
       n.last_health
FROM nodes n
LEFT JOIN node_blocklists b ON b.node_id = n.id
WHERE n.status IN ('online', 'degraded');
