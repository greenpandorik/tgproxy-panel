-- name: CreateTelemtUpdateJob :one
INSERT INTO telemt_update_jobs (node_id, from_version, to_version)
VALUES ($1, $2, $3) RETURNING *;

-- name: SetTelemtUpdateJobSteps :exec
UPDATE telemt_update_jobs SET steps = $2 WHERE id = $1;

-- name: FinishTelemtUpdateJob :one
UPDATE telemt_update_jobs SET status = $2, outcome = $3, error = $4, steps = $5,
  from_version = COALESCE(NULLIF(sqlc.arg('from_version')::text, ''), from_version),
  finished_at = now()
WHERE id = $1 RETURNING *;

-- name: SetTelemtUpdateJobVerification :exec
UPDATE telemt_update_jobs SET verification = $2 WHERE id = $1;

-- name: GetTelemtUpdateJob :one
SELECT * FROM telemt_update_jobs WHERE id = $1;

-- name: ListNodeTelemtUpdateJobs :many
SELECT * FROM telemt_update_jobs WHERE node_id = $1 ORDER BY started_at DESC LIMIT $2;

-- GetRunningTelemtUpdateJob is what refuses a second update while one is in flight.
-- name: GetRunningTelemtUpdateJob :one
SELECT * FROM telemt_update_jobs WHERE node_id = $1 AND finished_at IS NULL
ORDER BY started_at DESC LIMIT 1;

-- name: SetNodeTelemtUpdateAvailable :exec
UPDATE nodes SET telemt_update_available = $2 WHERE id = $1;
