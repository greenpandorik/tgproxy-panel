-- +goose Up

-- One telemt update, from the moment the panel started it to the state the node ended in.
-- steps is the maintenance sequence as the node reported it, so an operator can see where a
-- run stopped rather than only that it failed. outcome distinguishes a node that recovered
-- from one whose rollback also failed.
CREATE TABLE telemt_update_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'running',
  outcome text NOT NULL DEFAULT '',
  from_version text NOT NULL DEFAULT '',
  to_version text NOT NULL DEFAULT '',
  error text NOT NULL DEFAULT '',
  steps jsonb NOT NULL DEFAULT '[]'::jsonb,
  verification jsonb,
  started_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX telemt_update_jobs_node_idx ON telemt_update_jobs(node_id, started_at DESC);

-- +goose Down
DROP TABLE telemt_update_jobs;
