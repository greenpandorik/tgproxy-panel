-- The WEB counters are raw cumulative totals as telemt reports them, and telemt restarts
-- reset them to zero. Endpoint arithmetic (last - first) therefore reads a restarted node as
-- negative and throws away the traffic before the restart as well; these queries sum the
-- consecutive step-to-step deltas instead and clamp each step at zero, so a restart costs only
-- the single interval that spans it and is reported separately as counter_resets.
--
-- Absence is carried by the pair counts, never by a zero: a family the node never reported has
-- no row where both the sample and its predecessor are non-NULL, so its *_pairs is 0 and the
-- caller renders null. A family that was reported and did not move has pairs > 0 and a delta
-- of 0, which is a real measurement.
-- name: WebCounterWindow :one
WITH s AS (
  SELECT taken_at,
         web_carrier_selections_https AS selections_https,
         lag(web_carrier_selections_https) OVER (ORDER BY taken_at) AS prev_selections_https,
         web_carrier_selections_https_lanes AS selections_https_lanes,
         lag(web_carrier_selections_https_lanes) OVER (ORDER BY taken_at) AS prev_selections_https_lanes,
         web_carrier_selections_websocket AS selections_websocket,
         lag(web_carrier_selections_websocket) OVER (ORDER BY taken_at) AS prev_selections_websocket,
         web_carrier_selections_websocket_lanes AS selections_websocket_lanes,
         lag(web_carrier_selections_websocket_lanes) OVER (ORDER BY taken_at) AS prev_selections_websocket_lanes,
         web_carrier_failures AS failures,
         lag(web_carrier_failures) OVER (ORDER BY taken_at) AS prev_failures,
         web_rejected_attempts AS rejections,
         lag(web_rejected_attempts) OVER (ORDER BY taken_at) AS prev_rejections,
         web_evicted_sessions AS closures,
         lag(web_evicted_sessions) OVER (ORDER BY taken_at) AS prev_closures,
         web_bridge_recoveries AS bridge_recoveries,
         lag(web_bridge_recoveries) OVER (ORDER BY taken_at) AS prev_bridge_recoveries,
         node_id
  FROM node_stats_snapshots
  WHERE node_id = sqlc.arg('node_id') AND taken_at >= sqlc.arg('from_at') AND taken_at <= sqlc.arg('to_at')
)
SELECT
  count(*) FILTER (WHERE selections_https IS NOT NULL AND prev_selections_https IS NOT NULL)::bigint AS selections_https_pairs,
  coalesce(sum(GREATEST(selections_https - prev_selections_https, 0)) FILTER (WHERE selections_https IS NOT NULL AND prev_selections_https IS NOT NULL), 0)::bigint AS selections_https_delta,
  count(*) FILTER (WHERE selections_https_lanes IS NOT NULL AND prev_selections_https_lanes IS NOT NULL)::bigint AS selections_https_lanes_pairs,
  coalesce(sum(GREATEST(selections_https_lanes - prev_selections_https_lanes, 0)) FILTER (WHERE selections_https_lanes IS NOT NULL AND prev_selections_https_lanes IS NOT NULL), 0)::bigint AS selections_https_lanes_delta,
  count(*) FILTER (WHERE selections_websocket IS NOT NULL AND prev_selections_websocket IS NOT NULL)::bigint AS selections_websocket_pairs,
  coalesce(sum(GREATEST(selections_websocket - prev_selections_websocket, 0)) FILTER (WHERE selections_websocket IS NOT NULL AND prev_selections_websocket IS NOT NULL), 0)::bigint AS selections_websocket_delta,
  count(*) FILTER (WHERE selections_websocket_lanes IS NOT NULL AND prev_selections_websocket_lanes IS NOT NULL)::bigint AS selections_websocket_lanes_pairs,
  coalesce(sum(GREATEST(selections_websocket_lanes - prev_selections_websocket_lanes, 0)) FILTER (WHERE selections_websocket_lanes IS NOT NULL AND prev_selections_websocket_lanes IS NOT NULL), 0)::bigint AS selections_websocket_lanes_delta,
  count(*) FILTER (WHERE failures IS NOT NULL AND prev_failures IS NOT NULL)::bigint AS failures_pairs,
  coalesce(sum(GREATEST(failures - prev_failures, 0)) FILTER (WHERE failures IS NOT NULL AND prev_failures IS NOT NULL), 0)::bigint AS failures_delta,
  count(*) FILTER (WHERE rejections IS NOT NULL AND prev_rejections IS NOT NULL)::bigint AS rejections_pairs,
  coalesce(sum(GREATEST(rejections - prev_rejections, 0)) FILTER (WHERE rejections IS NOT NULL AND prev_rejections IS NOT NULL), 0)::bigint AS rejections_delta,
  count(*) FILTER (WHERE closures IS NOT NULL AND prev_closures IS NOT NULL)::bigint AS closures_pairs,
  coalesce(sum(GREATEST(closures - prev_closures, 0)) FILTER (WHERE closures IS NOT NULL AND prev_closures IS NOT NULL), 0)::bigint AS closures_delta,
  count(*) FILTER (WHERE bridge_recoveries IS NOT NULL AND prev_bridge_recoveries IS NOT NULL)::bigint AS bridge_recoveries_pairs,
  coalesce(sum(GREATEST(bridge_recoveries - prev_bridge_recoveries, 0)) FILTER (WHERE bridge_recoveries IS NOT NULL AND prev_bridge_recoveries IS NOT NULL), 0)::bigint AS bridge_recoveries_delta,
  count(*) FILTER (WHERE (selections_https < prev_selections_https)
    OR (selections_https_lanes < prev_selections_https_lanes)
    OR (selections_websocket < prev_selections_websocket)
    OR (selections_websocket_lanes < prev_selections_websocket_lanes)
    OR (failures < prev_failures)
    OR (rejections < prev_rejections)
    OR (closures < prev_closures)
    OR (bridge_recoveries < prev_bridge_recoveries))::bigint AS counter_resets,
  count(*)::bigint AS samples
FROM s;

-- name: WebCounterWindowAllNodes :one
WITH s AS (
  SELECT taken_at,
         web_carrier_selections_https AS selections_https,
         lag(web_carrier_selections_https) OVER (PARTITION BY node_id ORDER BY taken_at) AS prev_selections_https,
         web_carrier_selections_https_lanes AS selections_https_lanes,
         lag(web_carrier_selections_https_lanes) OVER (PARTITION BY node_id ORDER BY taken_at) AS prev_selections_https_lanes,
         web_carrier_selections_websocket AS selections_websocket,
         lag(web_carrier_selections_websocket) OVER (PARTITION BY node_id ORDER BY taken_at) AS prev_selections_websocket,
         web_carrier_selections_websocket_lanes AS selections_websocket_lanes,
         lag(web_carrier_selections_websocket_lanes) OVER (PARTITION BY node_id ORDER BY taken_at) AS prev_selections_websocket_lanes,
         web_carrier_failures AS failures,
         lag(web_carrier_failures) OVER (PARTITION BY node_id ORDER BY taken_at) AS prev_failures,
         web_rejected_attempts AS rejections,
         lag(web_rejected_attempts) OVER (PARTITION BY node_id ORDER BY taken_at) AS prev_rejections,
         web_evicted_sessions AS closures,
         lag(web_evicted_sessions) OVER (PARTITION BY node_id ORDER BY taken_at) AS prev_closures,
         web_bridge_recoveries AS bridge_recoveries,
         lag(web_bridge_recoveries) OVER (PARTITION BY node_id ORDER BY taken_at) AS prev_bridge_recoveries,
         node_id
  FROM node_stats_snapshots
  WHERE taken_at >= sqlc.arg('from_at') AND taken_at <= sqlc.arg('to_at')
)
SELECT
  count(*) FILTER (WHERE selections_https IS NOT NULL AND prev_selections_https IS NOT NULL)::bigint AS selections_https_pairs,
  coalesce(sum(GREATEST(selections_https - prev_selections_https, 0)) FILTER (WHERE selections_https IS NOT NULL AND prev_selections_https IS NOT NULL), 0)::bigint AS selections_https_delta,
  count(*) FILTER (WHERE selections_https_lanes IS NOT NULL AND prev_selections_https_lanes IS NOT NULL)::bigint AS selections_https_lanes_pairs,
  coalesce(sum(GREATEST(selections_https_lanes - prev_selections_https_lanes, 0)) FILTER (WHERE selections_https_lanes IS NOT NULL AND prev_selections_https_lanes IS NOT NULL), 0)::bigint AS selections_https_lanes_delta,
  count(*) FILTER (WHERE selections_websocket IS NOT NULL AND prev_selections_websocket IS NOT NULL)::bigint AS selections_websocket_pairs,
  coalesce(sum(GREATEST(selections_websocket - prev_selections_websocket, 0)) FILTER (WHERE selections_websocket IS NOT NULL AND prev_selections_websocket IS NOT NULL), 0)::bigint AS selections_websocket_delta,
  count(*) FILTER (WHERE selections_websocket_lanes IS NOT NULL AND prev_selections_websocket_lanes IS NOT NULL)::bigint AS selections_websocket_lanes_pairs,
  coalesce(sum(GREATEST(selections_websocket_lanes - prev_selections_websocket_lanes, 0)) FILTER (WHERE selections_websocket_lanes IS NOT NULL AND prev_selections_websocket_lanes IS NOT NULL), 0)::bigint AS selections_websocket_lanes_delta,
  count(*) FILTER (WHERE failures IS NOT NULL AND prev_failures IS NOT NULL)::bigint AS failures_pairs,
  coalesce(sum(GREATEST(failures - prev_failures, 0)) FILTER (WHERE failures IS NOT NULL AND prev_failures IS NOT NULL), 0)::bigint AS failures_delta,
  count(*) FILTER (WHERE rejections IS NOT NULL AND prev_rejections IS NOT NULL)::bigint AS rejections_pairs,
  coalesce(sum(GREATEST(rejections - prev_rejections, 0)) FILTER (WHERE rejections IS NOT NULL AND prev_rejections IS NOT NULL), 0)::bigint AS rejections_delta,
  count(*) FILTER (WHERE closures IS NOT NULL AND prev_closures IS NOT NULL)::bigint AS closures_pairs,
  coalesce(sum(GREATEST(closures - prev_closures, 0)) FILTER (WHERE closures IS NOT NULL AND prev_closures IS NOT NULL), 0)::bigint AS closures_delta,
  count(*) FILTER (WHERE bridge_recoveries IS NOT NULL AND prev_bridge_recoveries IS NOT NULL)::bigint AS bridge_recoveries_pairs,
  coalesce(sum(GREATEST(bridge_recoveries - prev_bridge_recoveries, 0)) FILTER (WHERE bridge_recoveries IS NOT NULL AND prev_bridge_recoveries IS NOT NULL), 0)::bigint AS bridge_recoveries_delta,
  count(*) FILTER (WHERE (selections_https < prev_selections_https)
    OR (selections_https_lanes < prev_selections_https_lanes)
    OR (selections_websocket < prev_selections_websocket)
    OR (selections_websocket_lanes < prev_selections_websocket_lanes)
    OR (failures < prev_failures)
    OR (rejections < prev_rejections)
    OR (closures < prev_closures)
    OR (bridge_recoveries < prev_bridge_recoveries))::bigint AS counter_resets,
  count(*)::bigint AS samples
FROM s;

-- WebLearningEntries reads the learning table size, which is a gauge and not a counter: the
-- window's answer is each node's last reading, summed over the nodes that reported one. nodes
-- is what tells absence from an empty table - zero nodes reporting is "not available".
-- name: WebLearningEntries :one
SELECT coalesce(sum(v), 0)::bigint AS entries, count(*)::bigint AS nodes FROM (
  SELECT DISTINCT ON (node_id) web_learning_entries AS v
  FROM node_stats_snapshots
  WHERE node_id = sqlc.arg('node_id') AND taken_at >= sqlc.arg('from_at') AND taken_at <= sqlc.arg('to_at')
    AND web_learning_entries IS NOT NULL
  ORDER BY node_id, taken_at DESC
) t;

-- name: WebLearningEntriesAllNodes :one
SELECT coalesce(sum(v), 0)::bigint AS entries, count(*)::bigint AS nodes FROM (
  SELECT DISTINCT ON (node_id) web_learning_entries AS v
  FROM node_stats_snapshots
  WHERE taken_at >= sqlc.arg('from_at') AND taken_at <= sqlc.arg('to_at')
    AND web_learning_entries IS NOT NULL
  ORDER BY node_id, taken_at DESC
) t;
