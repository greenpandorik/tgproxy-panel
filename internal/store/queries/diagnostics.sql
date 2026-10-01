-- name: InsertNodeDiagnostics :one
INSERT INTO node_diagnostics (node_id, started_at, finished_at, overall_status, trigger, checks)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListNodeDiagnostics :many
SELECT * FROM node_diagnostics WHERE node_id = $1 ORDER BY started_at DESC LIMIT $2;

-- LatestCertChecks is, per node, the certificate_expiry check of its newest diagnostics pass that
-- measured one.
-- name: LatestCertChecks :many
SELECT n.id AS node_id, d.started_at, d.cert::jsonb AS cert
FROM nodes n
JOIN LATERAL (
  SELECT started_at, jsonb_path_query_first(checks, '$[*].checks[*] ? (@.key == "certificate_expiry" && @.value != null)') AS cert
  FROM node_diagnostics
  WHERE node_id = n.id
    AND jsonb_path_exists(checks, '$[*].checks[*] ? (@.key == "certificate_expiry" && @.value != null)')
  ORDER BY started_at DESC LIMIT 1
) d ON true;
