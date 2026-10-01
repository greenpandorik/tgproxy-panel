-- +goose Up
ALTER TABLE node_stats_snapshots ADD COLUMN people_online int;
CREATE INDEX node_stats_taken_at_idx ON node_stats_snapshots(taken_at);

CREATE TABLE fleet_stats_snapshots (
  id bigserial PRIMARY KEY,
  taken_at timestamptz NOT NULL DEFAULT now(),
  people_online int NOT NULL,
  people_15m int NOT NULL,
  connections int NOT NULL
);
CREATE INDEX fleet_stats_taken_at_idx ON fleet_stats_snapshots(taken_at);

CREATE TABLE key_presence (
  access_key_id uuid PRIMARY KEY REFERENCES access_keys(id) ON DELETE CASCADE,
  connections int NOT NULL DEFAULT 0,
  devices int NOT NULL DEFAULT 0,
  devices_15m int NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE key_presence;
DROP TABLE fleet_stats_snapshots;
DROP INDEX node_stats_taken_at_idx;
ALTER TABLE node_stats_snapshots DROP COLUMN people_online;
