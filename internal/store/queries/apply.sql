-- name: CreateApplyJob :one
INSERT INTO apply_jobs (node_id, kind, status, started_at) VALUES ($1, $2, 'running', now()) RETURNING *;

-- name: FinishApplyJob :exec
UPDATE apply_jobs SET status = $2, finished_at = now(), error = $3, log = $4 WHERE id = $1;

-- name: ListNodeApplyJobs :many
SELECT * FROM apply_jobs WHERE node_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: ListRecentApplyJobs :many
SELECT j.*, n.name AS node_name FROM apply_jobs j JOIN nodes n ON n.id = j.node_id ORDER BY j.created_at DESC LIMIT $1;

-- name: GetNodeSite :one
SELECT * FROM node_sites WHERE node_id = $1;

-- SetNodeSiteDeployed records the hash of the bundle an apply actually pushed.
-- The bundle_hash = @hash guard is what makes it safe against a re-assign that
-- lands mid-apply: if node_sites now holds a different bundle, the update
-- matches no row, deployed_hash keeps its old value and the next sweep pushes
-- the new bundle for real. Writing unconditionally would mark a bundle deployed
-- that was never sent, with no recovery path through the UI.
-- name: SetNodeSiteDeployed :exec
UPDATE node_sites SET deployed_hash = @hash WHERE node_id = @node_id AND bundle_hash = @hash;
