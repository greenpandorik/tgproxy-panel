-- InsertSnapshot's dc_latency is coalesced so a caller with no DC data (a tproxy node, or a
-- Go nil) lands the column's '{}' rather than a NULL the NOT NULL constraint would reject.
-- name: InsertSnapshot :exec
INSERT INTO node_stats_snapshots (node_id, sessions_live, streams_live, bytes_up, bytes_down, sessions_created, limit_hits, mtproxy_raw, relay_raw,
  cpu_percent, mem_used_percent, disk_used_percent, dc_latency)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, coalesce(sqlc.narg('dc_latency')::jsonb, '{}'::jsonb));

-- name: ListSnapshots :many
SELECT * FROM node_stats_snapshots WHERE node_id = $1 AND taken_at >= $2 AND taken_at <= $3 ORDER BY taken_at;

-- name: ListSnapshotsAllNodes :many
SELECT node_id, taken_at, sessions_live, streams_live, bytes_up, bytes_down, cpu_percent, mem_used_percent, disk_used_percent, dc_latency
FROM node_stats_snapshots WHERE taken_at >= $1 AND taken_at <= $2 ORDER BY node_id, taken_at;

-- ListSnapshotsAllNodesBucketed collapses snapshots into fixed-width time buckets in the
-- database. The unbucketed query returns one row per node per minute, so a wide range is
-- bounded only by retention (30 days x 1440/day x N nodes) and the whole set has to be
-- materialised in Go before it can be thinned. Bucketing first makes the result size
-- O(range/step) per node instead.
--
-- sessions_live/streams_live are gauges, so the bucket's average is the honest summary;
-- bytes_up/bytes_down are monotonic counters, so the bucket's max is its closing value and
-- consecutive maxima give the correct average rate over the interval between the buckets'
-- closing timestamps. The point's own timestamp is max(taken_at) rather than the bucket
-- boundary for exactly that reason: a partial trailing bucket would otherwise be divided by
-- the full step width and under-report the current rate. The load percentages are gauges
-- like the session counts and get the same average. node_stats_node_time_idx
-- (node_id, taken_at DESC) covers the scan.
--
-- dc_latency is a {"<dc>": <ms>} object per row, averaged per DC over the rows of the bucket
-- that carry that DC (a row without the key is "unknown", not 0, so it must not drag the
-- mean down). That is a second pass over the same rows: the keys are unnested with
-- jsonb_each, averaged per (node, bucket, dc) and folded back into one object per bucket,
-- then joined onto the scalar aggregates by bucket. A bucket none of whose rows measured any
-- DC gets '{}'.
-- name: ListSnapshotsAllNodesBucketed :many
WITH scalars AS (
  SELECT r.node_id,
         floor(extract(epoch FROM r.taken_at) / sqlc.arg('step')::bigint) AS bucket,
         max(r.taken_at)::timestamptz AS taken_at,
         round(avg(r.sessions_live))::int AS sessions_live,
         round(avg(r.streams_live))::int AS streams_live,
         max(r.bytes_up)::bigint AS bytes_up,
         max(r.bytes_down)::bigint AS bytes_down,
         avg(r.cpu_percent)::real AS cpu_percent,
         avg(r.mem_used_percent)::real AS mem_used_percent,
         avg(r.disk_used_percent)::real AS disk_used_percent
  FROM node_stats_snapshots r
  WHERE r.taken_at >= sqlc.arg('from_at') AND r.taken_at <= sqlc.arg('to_at')
  GROUP BY r.node_id, 2
), per_dc AS (
  SELECT s.node_id,
         floor(extract(epoch FROM s.taken_at) / sqlc.arg('step')::bigint) AS bucket,
         d.key AS dc,
         avg((d.value #>> '{}')::float8) AS latency_ms
  FROM node_stats_snapshots s, jsonb_each(s.dc_latency) AS d(key, value)
  WHERE s.taken_at >= sqlc.arg('from_at') AND s.taken_at <= sqlc.arg('to_at')
  GROUP BY s.node_id, 2, d.key
), dc AS (
  SELECT node_id, bucket, jsonb_object_agg(dc, latency_ms) AS dc_latency
  FROM per_dc GROUP BY node_id, bucket
)
SELECT scalars.node_id,
       scalars.taken_at,
       scalars.sessions_live,
       scalars.streams_live,
       scalars.bytes_up,
       scalars.bytes_down,
       scalars.cpu_percent,
       scalars.mem_used_percent,
       scalars.disk_used_percent,
       coalesce(dc.dc_latency, '{}'::jsonb)::jsonb AS dc_latency
FROM scalars
LEFT JOIN dc ON dc.node_id = scalars.node_id AND dc.bucket = scalars.bucket
ORDER BY scalars.node_id, scalars.taken_at;

-- name: LatestSnapshots :many
SELECT DISTINCT ON (node_id) * FROM node_stats_snapshots ORDER BY node_id, taken_at DESC;

-- name: DeleteOldSnapshots :exec
DELETE FROM node_stats_snapshots WHERE taken_at < $1;

-- name: InsertAlert :one
INSERT INTO alerts (node_id, kind, message) VALUES ($1, $2, $3) RETURNING *;

-- ResolveNodeAlerts reports the number of rows it actually closed (:execrows)
-- so callers can tell "an alert was open and just got resolved" from "there was
-- nothing to resolve" without a separate SELECT.
-- name: ResolveNodeAlerts :execrows
UPDATE alerts SET resolved_at = now() WHERE node_id = $1 AND kind = $2 AND resolved_at IS NULL;

-- name: ListOpenAlerts :many
SELECT a.*, n.name AS node_name FROM alerts a LEFT JOIN nodes n ON n.id = a.node_id WHERE a.resolved_at IS NULL ORDER BY a.created_at DESC;

-- name: ResolveAlert :exec
UPDATE alerts SET resolved_at = now() WHERE id = $1;

-- key_stats_snapshots hold per-key traffic/connection counters read from telemt
-- nodes. They are written by the stats worker and read by the key drawer.
-- name: InsertKeyStatsSnapshot :exec
INSERT INTO key_stats_snapshots (access_key_id, node_id, connections, total_octets, quota_used_bytes, active_ips)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListKeyStatsSnapshots :many
SELECT s.*, n.name AS node_name FROM key_stats_snapshots s JOIN nodes n ON n.id = s.node_id
WHERE s.access_key_id = $1 AND s.taken_at >= $2 AND s.taken_at <= $3
ORDER BY s.node_id, s.taken_at;

-- ListKeyStatsSnapshotsBucketed is ListKeyStatsSnapshots with the thinning done in the
-- database, mirroring ListSnapshotsAllNodesBucketed. The unbucketed query returns one row per
-- key per node per minute, so a 31-day window is ~45k rows per node that Go loads in full only
-- to throw all but 1000 of them away. Bucketing first makes the result O(range/step) per node.
--
-- connections is a gauge, so the bucket's average is the honest summary; total_octets is a
-- monotonic counter, so the bucket's max is its closing value and consecutive maxima give the
-- correct traffic between two buckets. The point's timestamp is max(taken_at), not the bucket
-- boundary, so a partial trailing bucket is placed where its data actually ends.
-- name: ListKeyStatsSnapshotsBucketed :many
SELECT s.node_id,
       n.name AS node_name,
       max(s.taken_at)::timestamptz AS taken_at,
       round(avg(s.connections))::int AS connections,
       max(s.total_octets)::bigint AS total_octets
FROM key_stats_snapshots s JOIN nodes n ON n.id = s.node_id
WHERE s.access_key_id = sqlc.arg('access_key_id')
  AND s.taken_at >= sqlc.arg('from_at') AND s.taken_at <= sqlc.arg('to_at')
GROUP BY s.node_id, n.name, floor(extract(epoch FROM s.taken_at) / sqlc.arg('step')::bigint)
ORDER BY s.node_id, 3;

-- KeyTrafficLast30d reports, per key, how many octets the key moved inside the window: the sum
-- of the positive step-to-step deltas of total_octets on each node, summed over the nodes.
--
-- total_octets is a *process-scoped* cumulative counter, so it restarts at zero whenever telemt
-- does. Taking the endpoints (last - first) therefore reads a restarted node as negative and
-- clamps the whole window to zero, discarding the traffic before the restart as well as after
-- it. Summing consecutive deltas and clamping each one instead keeps every complete interval
-- and loses only the single step that spans the restart. lag() over (key, node) ordered by time
-- gives the previous reading; the first row of each run has no predecessor and GREATEST ignores
-- the resulting NULL, so it contributes nothing.
-- The whole page of keys is answered by one query - the keys list must not fan out per row.
-- name: KeyTrafficLast30d :many
WITH deltas AS (
  SELECT access_key_id,
         GREATEST(total_octets - lag(total_octets) OVER (
           PARTITION BY access_key_id, node_id ORDER BY taken_at), 0) AS octets
  FROM key_stats_snapshots
  WHERE access_key_id = ANY(sqlc.arg('key_ids')::uuid[]) AND taken_at >= sqlc.arg('since')
)
SELECT access_key_id, coalesce(sum(octets), 0)::bigint AS traffic FROM deltas GROUP BY access_key_id;

-- name: LatestKeyStatsSnapshots :many
SELECT DISTINCT ON (node_id) * FROM key_stats_snapshots WHERE access_key_id = $1 ORDER BY node_id, taken_at DESC;

-- DeleteOldKeyStatsSnapshots removes one bounded batch of expired rows and reports how many it
-- deleted, so the caller can loop until the sweep is done. An unbounded DELETE over the largest
-- table in the database takes a single long-running statement whose locks and WAL burst are
-- felt by every other query; a batch keeps each statement short. key_stats_taken_at_idx
-- (migration 00006) is what makes the inner SELECT an index scan rather than a seq scan.
-- name: DeleteOldKeyStatsSnapshots :execrows
DELETE FROM key_stats_snapshots s
WHERE s.id IN (
  SELECT b.id FROM key_stats_snapshots b
  WHERE b.taken_at < sqlc.arg('before') ORDER BY b.taken_at LIMIT sqlc.arg('batch')
);
