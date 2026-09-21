-- +goose Up

-- Traffic and processor readings that were never taken used to be stored as zero, because the
-- columns could not say anything else. Consumption is the difference between two rows, so a zero
-- in the middle of a series is not a lost reading but a wrong one: 100 -> 0 -> 110 is charged as
-- 110 rather than 10, on the node's chart and against the key's quota both.
--
-- Dropping NOT NULL lets a row say "not measured". Existing rows keep their values: every one of
-- them was a real reading, and nothing here reinterprets them.
ALTER TABLE node_stats_snapshots
  ALTER COLUMN bytes_up DROP NOT NULL,
  ALTER COLUMN bytes_down DROP NOT NULL,
  ALTER COLUMN cpu_percent DROP NOT NULL;

ALTER TABLE key_stats_snapshots
  ALTER COLUMN total_octets DROP NOT NULL,
  ALTER COLUMN quota_used_bytes DROP NOT NULL;

-- cpu_percent holds a one-minute load average divided by core count, which is not processor
-- utilisation: a node can sit at load 8 with an idle processor waiting on disk. The new column
-- holds the measured thing, and the old one keeps its name and its history rather than being
-- retroactively relabelled as something it never was.
ALTER TABLE node_stats_snapshots
  ADD COLUMN cpu_utilisation_percent real,
  ADD COLUMN load_average_1 real;

-- +goose Down
ALTER TABLE node_stats_snapshots
  DROP COLUMN cpu_utilisation_percent,
  DROP COLUMN load_average_1;

UPDATE node_stats_snapshots SET bytes_up = 0 WHERE bytes_up IS NULL;
UPDATE node_stats_snapshots SET bytes_down = 0 WHERE bytes_down IS NULL;
UPDATE node_stats_snapshots SET cpu_percent = 0 WHERE cpu_percent IS NULL;
UPDATE key_stats_snapshots SET total_octets = 0 WHERE total_octets IS NULL;
UPDATE key_stats_snapshots SET quota_used_bytes = 0 WHERE quota_used_bytes IS NULL;

ALTER TABLE node_stats_snapshots
  ALTER COLUMN bytes_up SET NOT NULL,
  ALTER COLUMN bytes_down SET NOT NULL,
  ALTER COLUMN cpu_percent SET NOT NULL;

ALTER TABLE key_stats_snapshots
  ALTER COLUMN total_octets SET NOT NULL,
  ALTER COLUMN quota_used_bytes SET NOT NULL;
