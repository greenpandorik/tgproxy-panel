-- +goose Up
CREATE TABLE fleet_rollouts (
 id uuid PRIMARY KEY, status text NOT NULL DEFAULT 'running', request jsonb NOT NULL,
 node_ids jsonb NOT NULL, cursor integer NOT NULL DEFAULT 0,
 dispatch_at timestamptz, job_id uuid REFERENCES telemt_update_jobs(id),
 error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX one_active_rollout ON fleet_rollouts((true)) WHERE status='running';
-- +goose Down
DROP TABLE fleet_rollouts;
