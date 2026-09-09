-- +goose Up

-- Panel-computed capabilities and the WEB policy the node should be running.
-- telemt_capabilities is nullable on purpose: NULL means "not determined yet", which is a
-- different thing from a node that supports nothing.
ALTER TABLE nodes ADD COLUMN telemt_build text NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN telemt_capabilities jsonb;
ALTER TABLE nodes ADD COLUMN telemt_capabilities_checked_at timestamptz;
ALTER TABLE nodes ADD COLUMN telemt_update_available text NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN telemt_web_policy jsonb NOT NULL DEFAULT '{}'::jsonb;

-- Raw cumulative WEB counters as telemt reports them. Every column is nullable because a
-- metric family absent from the node's output must stay distinguishable from a real zero;
-- deltas are computed at read time and an interval where a counter went backwards is a
-- telemt restart, not negative traffic.
ALTER TABLE node_stats_snapshots ADD COLUMN web_carrier_selections_https bigint;
ALTER TABLE node_stats_snapshots ADD COLUMN web_carrier_selections_https_lanes bigint;
ALTER TABLE node_stats_snapshots ADD COLUMN web_carrier_selections_websocket bigint;
ALTER TABLE node_stats_snapshots ADD COLUMN web_carrier_selections_websocket_lanes bigint;
ALTER TABLE node_stats_snapshots ADD COLUMN web_carrier_failures bigint;
ALTER TABLE node_stats_snapshots ADD COLUMN web_rejected_attempts bigint;
ALTER TABLE node_stats_snapshots ADD COLUMN web_evicted_sessions bigint;
ALTER TABLE node_stats_snapshots ADD COLUMN web_bridge_recoveries bigint;
ALTER TABLE node_stats_snapshots ADD COLUMN web_learning_entries int;

CREATE TABLE node_diagnostics (
  id bigserial PRIMARY KEY,
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  started_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  overall_status text NOT NULL,
  trigger text NOT NULL,
  checks jsonb NOT NULL DEFAULT '[]'::jsonb
);
CREATE INDEX node_diagnostics_node_time_idx ON node_diagnostics(node_id, started_at DESC);

-- +goose Down
DROP TABLE node_diagnostics;
ALTER TABLE node_stats_snapshots DROP COLUMN web_learning_entries;
ALTER TABLE node_stats_snapshots DROP COLUMN web_bridge_recoveries;
ALTER TABLE node_stats_snapshots DROP COLUMN web_evicted_sessions;
ALTER TABLE node_stats_snapshots DROP COLUMN web_rejected_attempts;
ALTER TABLE node_stats_snapshots DROP COLUMN web_carrier_failures;
ALTER TABLE node_stats_snapshots DROP COLUMN web_carrier_selections_websocket_lanes;
ALTER TABLE node_stats_snapshots DROP COLUMN web_carrier_selections_websocket;
ALTER TABLE node_stats_snapshots DROP COLUMN web_carrier_selections_https_lanes;
ALTER TABLE node_stats_snapshots DROP COLUMN web_carrier_selections_https;
ALTER TABLE nodes DROP COLUMN telemt_web_policy;
ALTER TABLE nodes DROP COLUMN telemt_update_available;
ALTER TABLE nodes DROP COLUMN telemt_capabilities_checked_at;
ALTER TABLE nodes DROP COLUMN telemt_capabilities;
ALTER TABLE nodes DROP COLUMN telemt_build;
