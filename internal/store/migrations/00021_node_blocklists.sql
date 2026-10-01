-- +goose Up
CREATE TABLE node_blocklists (
  node_id uuid PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  entries jsonb NOT NULL DEFAULT '[]',
  revision bigint NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE node_blocklists;
