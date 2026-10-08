-- CreateNode takes the engine and its Fake-TLS settings as nullable arguments so
-- that a caller which does not care about them (tests, fixtures) still gets the
-- column defaults rather than an invalid enum value or a zero port.
-- name: CreateNode :one
INSERT INTO nodes (name, hostname, public_ip, acme_email, install_token_hash, install_token_expires, engine, tls_domain, classic_port, sort_order)
VALUES ($1, $2, $3, $4, $5, $6,
  COALESCE(sqlc.narg('engine')::node_engine, 'telemt'),
  COALESCE(sqlc.narg('tls_domain')::text, ''),
  COALESCE(sqlc.narg('classic_port')::int, 8443),
  (SELECT COALESCE(max(sort_order), 0) + 1 FROM nodes))
RETURNING *;


-- name: GetNode :one
SELECT * FROM nodes WHERE id = $1;

-- name: GetNodeByHostname :one
SELECT * FROM nodes WHERE hostname = $1;

-- name: GetNodeByInstallToken :one
SELECT * FROM nodes WHERE install_token_hash = $1 AND install_token_expires > now();

-- name: GetNodeByAgentToken :one
SELECT * FROM nodes WHERE agent_token_hash = $1;

-- ListNodes is every node in the operator's order.
-- name: ListNodes :many
SELECT * FROM nodes ORDER BY sort_order, created_at;

-- name: UpdateNode :one
UPDATE nodes SET name = $2, public_ip = $3, max_profiles = $4, acme_email = $5,
  tls_domain = COALESCE(sqlc.narg('tls_domain')::text, tls_domain),
  classic_port = COALESCE(sqlc.narg('classic_port')::int, classic_port),
  ad_tag = COALESCE(sqlc.narg('ad_tag')::text, ad_tag),
  tls_domains = COALESCE(sqlc.narg('tls_domains')::text[], tls_domains)
WHERE id = $1 RETURNING *;

-- SetNodeWebPolicy stores only what the operator set differently from the panel default,
-- so an untouched node keeps an empty object and follows the default as it moves.
-- name: SetNodeWebPolicy :one
UPDATE nodes SET telemt_web_policy = $2 WHERE id = $1 RETURNING *;

-- name: SetNodeInstallToken :exec
UPDATE nodes SET install_token_hash = $2, install_token_expires = $3 WHERE id = $1;

-- RegisterNode consumes the install token and issues the agent token in one statement. The
-- token is checked again here, not only when the node was looked up: two concurrent
-- registrations with one token would otherwise both get an agent token, and the last write would
-- take over the node. Zero rows means the token was already spent or has expired. public_ip is
-- filled in only when the panel did not have one.
-- name: RegisterNode :execrows
UPDATE nodes SET agent_token_hash = sqlc.arg('agent_token_hash'),
  install_token_hash = NULL, install_token_expires = NULL,
  tproxy_version = sqlc.arg('tproxy_version'), agent_version = sqlc.arg('agent_version'), status = 'offline',
  public_ip = CASE WHEN public_ip = '' THEN sqlc.arg('public_ip')::text ELSE public_ip END
WHERE id = sqlc.arg('id') AND install_token_hash = sqlc.arg('install_token_hash')::text
  AND install_token_expires > now();

-- SetNodePublicIP sets the node's public address, which telemt needs for
-- web.vhosts.public_addr. Registration fills it in through RegisterNode.
-- name: SetNodePublicIP :exec
UPDATE nodes SET public_ip = $2 WHERE id = $1;

-- name: SetNodeTelemtVersion :exec
UPDATE nodes SET telemt_version = $2 WHERE id = $1;

-- SetNodeOnline records the versions the agent reported. An empty string means "the agent
-- could not read it" (telemt's version comes from its control API, which may be briefly
-- unreachable) and must never overwrite a known version with a blank. telemt_version is only
-- written for telemt nodes, so a tproxy node cannot end up labelled with one.
-- name: SetNodeOnline :exec
UPDATE nodes SET status = 'online', last_seen_at = now(),
  tproxy_version = COALESCE(NULLIF(sqlc.arg('tproxy_version')::text, ''), tproxy_version),
  agent_version = COALESCE(NULLIF(sqlc.arg('agent_version')::text, ''), agent_version),
  telemt_version = CASE WHEN engine = 'telemt'
    THEN COALESCE(NULLIF(sqlc.arg('telemt_version')::text, ''), telemt_version)
    ELSE telemt_version END
WHERE id = $1;

-- SetNodeHeartbeat carries the versions too: a telemt node whose control API was down at
-- hello time reports its version on the first heartbeat that reaches it, and there is no
-- other moment at which the panel would learn it.
--
-- telemt_capabilities is only written when the heartbeat carried a capability set: an agent
-- that could not probe the node sends none, and overwriting a known set with nothing would
-- turn "the node cannot do this" into "we never asked". checked_at moves with it for the same
-- reason - it dates the set that is stored, not the last heartbeat.
-- name: SetNodeHeartbeat :exec
UPDATE nodes SET status = $2, last_seen_at = now(), last_health = $3,
  tproxy_version = COALESCE(NULLIF(sqlc.arg('tproxy_version')::text, ''), tproxy_version),
  telemt_version = CASE WHEN engine = 'telemt'
    THEN COALESCE(NULLIF(sqlc.arg('telemt_version')::text, ''), telemt_version)
    ELSE telemt_version END,
  telemt_build = COALESCE(NULLIF(sqlc.arg('telemt_build')::text, ''), telemt_build),
  telemt_capabilities = COALESCE(sqlc.narg('telemt_capabilities')::jsonb, telemt_capabilities),
  telemt_capabilities_checked_at = CASE WHEN sqlc.narg('telemt_capabilities')::jsonb IS NOT NULL
    THEN now() ELSE telemt_capabilities_checked_at END
WHERE id = $1;

-- name: SetNodeStatus :exec
UPDATE nodes SET status = $2 WHERE id = $1;

-- MarkStaleNodesOffline also returns offline nodes without an open node_offline alert: a closed
-- agent stream marks its node offline at once, and without this such an outage never raised an
-- alert because the node was no longer online or degraded by the time it went stale.
-- name: MarkStaleNodesOffline :many
UPDATE nodes SET status = 'offline'
WHERE last_seen_at < $1
  AND (status IN ('online','degraded')
    OR (status = 'offline' AND NOT EXISTS (
      SELECT 1 FROM alerts a WHERE a.node_id = nodes.id AND a.kind = 'node_offline' AND a.resolved_at IS NULL)))
RETURNING id;

-- SetNodeDirty bumps dirty_seq whenever it marks a node dirty. An in-flight apply carries
-- the dirty_seq it snapshotted, so any mutation that lands while it runs makes the apply's
-- SetNodeApplied a no-op and the node stays dirty for the next sweep.
-- name: SetNodeDirty :exec
UPDATE nodes SET dirty = sqlc.arg('dirty'),
  dirty_seq = CASE WHEN sqlc.arg('dirty')::boolean THEN dirty_seq + 1 ELSE dirty_seq END,
  dirty_since = CASE WHEN sqlc.arg('dirty')::boolean THEN COALESCE(dirty_since, now()) ELSE NULL END
WHERE id = sqlc.arg('id');

-- name: SetNodeApplied :exec
UPDATE nodes SET dirty = false, dirty_since = NULL, last_apply_at = now()
WHERE id = sqlc.arg('id') AND dirty_seq = sqlc.arg('dirty_seq');

-- name: DeleteNode :exec
DELETE FROM nodes WHERE id = $1;

-- name: SetNodeLastCheck :exec
UPDATE nodes SET last_check = $2 WHERE id = $1;

-- name: CountNodesByStatus :many
SELECT status, count(*) AS n FROM nodes GROUP BY status;

-- ListNodesWithCounts is ListNodes plus each node's profile count, so the list
-- endpoint no longer issues one CountNodeProfiles round trip per node.
-- name: ListNodesWithCounts :many
SELECT sqlc.embed(n), (SELECT count(*) FROM profiles p WHERE p.node_id = n.id) AS profile_count
FROM nodes n ORDER BY n.sort_order, n.created_at;

-- ListDirtyNodesAny returns every dirty node regardless of status: the apply
-- sweep decides reachability from the live driver, so a node marked offline by a
-- stale heartbeat while its gRPC stream is up is still picked up.
-- name: ListDirtyNodesAny :many
SELECT * FROM nodes WHERE dirty = true;

-- ReorderNodes puts the listed nodes first, in the order given, and keeps every other node after
-- them in its current order. Ids that name no node are skipped.
-- name: ReorderNodes :execrows
WITH listed AS (
  SELECT t.id, t.ord FROM unnest(sqlc.arg('ids')::uuid[]) WITH ORDINALITY AS t(id, ord)
), ranked AS (
  SELECT n.id, row_number() OVER (ORDER BY l.ord IS NULL, l.ord, n.sort_order, n.created_at, n.id)::int AS pos
  FROM nodes n LEFT JOIN listed l ON l.id = n.id
)
UPDATE nodes n SET sort_order = r.pos FROM ranked r WHERE n.id = r.id AND n.sort_order <> r.pos;
