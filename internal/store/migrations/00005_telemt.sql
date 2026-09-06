-- +goose Up
-- The telemt engine replaces the tproxy-server + MTProxy pair with a single
-- telemt process. New nodes default to it; every node that already exists was
-- installed with the old stack, so the UPDATE below pins them to 'tproxy'
-- before any new row can be inserted.
CREATE TYPE node_engine AS ENUM ('tproxy', 'telemt');

ALTER TABLE nodes ADD COLUMN engine node_engine NOT NULL DEFAULT 'telemt';
UPDATE nodes SET engine = 'tproxy';

-- tls_domain is the SNI the Fake-TLS listener presents (and masks unknown SNI
-- towards); classic_port is the port that listener binds. Both are meaningless
-- for tproxy nodes and simply stay at their defaults there.
ALTER TABLE nodes ADD COLUMN tls_domain text NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN classic_port int NOT NULL DEFAULT 8443;
ALTER TABLE nodes ADD COLUMN telemt_version text NOT NULL DEFAULT '';

-- Per-key telemt limits, kept apart from `limits` (the tproxy relay's
-- per-profile session/stream limits): the two engines have disjoint limit
-- vocabularies and a key can be bound to nodes of both kinds at once.
ALTER TABLE access_keys ADD COLUMN telemt_limits jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE key_stats_snapshots (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  access_key_id uuid NOT NULL REFERENCES access_keys(id) ON DELETE CASCADE,
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  taken_at timestamptz NOT NULL DEFAULT now(),
  connections int NOT NULL DEFAULT 0,
  total_octets bigint NOT NULL DEFAULT 0,
  quota_used_bytes bigint NOT NULL DEFAULT 0,
  active_ips int NOT NULL DEFAULT 0
);
-- (access_key_id, taken_at DESC) covers the key drawer's per-key series. It does
-- NOT serve the retention sweep, which filters on taken_at alone: a composite
-- index is only usable for a predicate on its leading column. Migration 00006
-- adds the taken_at index that sweep needs.
CREATE INDEX key_stats_key_time_idx ON key_stats_snapshots(access_key_id, taken_at DESC);

-- +goose Down
DROP TABLE key_stats_snapshots;
ALTER TABLE access_keys DROP COLUMN telemt_limits;
ALTER TABLE nodes DROP COLUMN telemt_version;
ALTER TABLE nodes DROP COLUMN classic_port;
ALTER TABLE nodes DROP COLUMN tls_domain;
ALTER TABLE nodes DROP COLUMN engine;
DROP TYPE node_engine;
