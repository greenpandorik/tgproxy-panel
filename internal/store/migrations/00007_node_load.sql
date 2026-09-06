-- +goose Up
-- Server load per snapshot. The heartbeat already carries CPU / memory / disk
-- percentages into nodes.last_health, but that column only ever holds the
-- newest report, so the panel could show "how loaded is it now" and never
-- "how loaded has it been". The stats worker copies the figures it has at
-- snapshot time into these columns, which gives them the same one-minute
-- cadence and 30-day retention as the session and traffic series they sit
-- beside. `real` is plenty for a percentage.
ALTER TABLE node_stats_snapshots
  ADD COLUMN cpu_percent real NOT NULL DEFAULT 0,
  ADD COLUMN mem_used_percent real NOT NULL DEFAULT 0,
  ADD COLUMN disk_used_percent real NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE node_stats_snapshots
  DROP COLUMN cpu_percent,
  DROP COLUMN mem_used_percent,
  DROP COLUMN disk_used_percent;
