-- +goose Up
-- Latency to Telegram's datacenters per snapshot. The heartbeat carries telemt's per-DC
-- latency EMA into nodes.last_health, which only ever holds the newest report; copying it
-- into the snapshot at the stats worker's cadence gives the "how has DC 2 been from this
-- node" chart the same one-minute resolution and 30-day retention as the load series.
-- The shape is {"<dc>": <ms>}: DCs whose EMA telemt has not measured yet are left out, so
-- a missing key means "unknown" and never reads as 0 ms. A tproxy node (no DC data) and
-- a telemt node whose stats call failed at heartbeat time both write '{}'.
ALTER TABLE node_stats_snapshots
  ADD COLUMN dc_latency jsonb NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE node_stats_snapshots
  DROP COLUMN dc_latency;
