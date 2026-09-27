-- +goose Up
CREATE TABLE probe_reports (
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  location text NOT NULL,
  measured_at timestamptz NOT NULL,
  report jsonb NOT NULL,
  PRIMARY KEY(node_id, location)
);
-- +goose Down
DROP TABLE probe_reports;
