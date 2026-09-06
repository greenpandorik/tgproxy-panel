-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE admin_role AS ENUM ('owner','admin','viewer');
CREATE TYPE node_status AS ENUM ('pending','online','offline','degraded');
CREATE TYPE sync_state AS ENUM ('pending','synced','failed');
CREATE TYPE key_type AS ENUM ('SHARED','PERSONAL');
CREATE TYPE key_status AS ENUM ('pending','active','revoked');
CREATE TYPE apply_status AS ENUM ('queued','running','ok','failed','rolled_back');
CREATE TYPE apply_kind AS ENUM ('profiles','site','both');
CREATE TYPE backup_kind AS ENUM ('manual','scheduled');

CREATE TABLE admin_users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  username text NOT NULL UNIQUE,
  password_hash text NOT NULL,
  role admin_role NOT NULL DEFAULT 'admin',
  totp_secret_enc bytea,
  totp_enabled boolean NOT NULL DEFAULT false,
  failed_logins int NOT NULL DEFAULT 0,
  locked_until timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE recovery_codes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  admin_user_id uuid NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
  code_hash text NOT NULL,
  used_at timestamptz
);

CREATE TABLE sessions (
  id text PRIMARY KEY,
  admin_user_id uuid NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  ip text NOT NULL DEFAULT '',
  user_agent text NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_idx ON sessions(admin_user_id);

CREATE TABLE nodes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  hostname text NOT NULL UNIQUE,
  public_ip text NOT NULL DEFAULT '',
  acme_email text NOT NULL DEFAULT '',
  status node_status NOT NULL DEFAULT 'pending',
  agent_token_hash text,
  install_token_hash text,
  install_token_expires timestamptz,
  tproxy_version text NOT NULL DEFAULT '',
  agent_version text NOT NULL DEFAULT '',
  max_profiles int NOT NULL DEFAULT 128,
  dirty boolean NOT NULL DEFAULT false,
  last_seen_at timestamptz,
  last_apply_at timestamptz,
  last_health jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE access_keys (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  label text NOT NULL,
  type key_type NOT NULL,
  owner_label text NOT NULL DEFAULT '',
  secret_enc bytea NOT NULL,
  status key_status NOT NULL DEFAULT 'pending',
  carrier_mode text NOT NULL DEFAULT 'https',
  limits jsonb NOT NULL DEFAULT '{}'::jsonb,
  expires_at timestamptz,
  revoked_at timestamptz,
  note text NOT NULL DEFAULT '',
  created_by uuid REFERENCES admin_users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX access_keys_status_idx ON access_keys(status);

CREATE TABLE profiles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  access_key_id uuid REFERENCES access_keys(id) ON DELETE CASCADE,
  name text NOT NULL,
  secret_enc bytea NOT NULL,
  backend text NOT NULL DEFAULT '127.0.0.1:2398',
  carrier_mode text NOT NULL DEFAULT 'https',
  limits jsonb NOT NULL DEFAULT '{}'::jsonb,
  sync_state sync_state NOT NULL DEFAULT 'pending',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(node_id, name)
);
CREATE INDEX profiles_key_idx ON profiles(access_key_id);

CREATE TABLE key_bindings (
  access_key_id uuid NOT NULL REFERENCES access_keys(id) ON DELETE CASCADE,
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  PRIMARY KEY (access_key_id, node_id)
);

CREATE TABLE site_templates (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  html text NOT NULL,
  assets jsonb NOT NULL DEFAULT '{}'::jsonb,
  is_preset boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE node_sites (
  node_id uuid PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  template_id uuid REFERENCES site_templates(id) ON DELETE SET NULL,
  bundle jsonb NOT NULL,
  bundle_hash text NOT NULL,
  deployed_hash text,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE apply_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  status apply_status NOT NULL DEFAULT 'queued',
  kind apply_kind NOT NULL,
  started_at timestamptz,
  finished_at timestamptz,
  error text NOT NULL DEFAULT '',
  log text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX apply_jobs_node_idx ON apply_jobs(node_id, created_at DESC);

CREATE TABLE branding_profiles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  is_active boolean NOT NULL DEFAULT false,
  panel_name text NOT NULL DEFAULT 'WEB Proxy Panel',
  logo_path text NOT NULL DEFAULT '',
  favicon_path text NOT NULL DEFAULT '',
  primary_color text NOT NULL DEFAULT '#3b82f6',
  accent_color text NOT NULL DEFAULT '#22c55e',
  theme_default text NOT NULL DEFAULT 'dark',
  login_bg_path text NOT NULL DEFAULT '',
  login_text text NOT NULL DEFAULT '',
  support_link text NOT NULL DEFAULT '',
  footer_text text NOT NULL DEFAULT '',
  custom_css text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX branding_profiles_one_active ON branding_profiles(is_active) WHERE is_active;
INSERT INTO branding_profiles (name, is_active) VALUES ('default', true);

CREATE TABLE audit_log (
  id bigserial PRIMARY KEY,
  admin_user_id uuid REFERENCES admin_users(id) ON DELETE SET NULL,
  action text NOT NULL,
  target_type text NOT NULL DEFAULT '',
  target_id text NOT NULL DEFAULT '',
  meta jsonb NOT NULL DEFAULT '{}'::jsonb,
  ip text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_created_idx ON audit_log(created_at DESC);

CREATE TABLE node_stats_snapshots (
  id bigserial PRIMARY KEY,
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  taken_at timestamptz NOT NULL DEFAULT now(),
  sessions_live int NOT NULL DEFAULT 0,
  streams_live int NOT NULL DEFAULT 0,
  bytes_up bigint NOT NULL DEFAULT 0,
  bytes_down bigint NOT NULL DEFAULT 0,
  sessions_created bigint NOT NULL DEFAULT 0,
  limit_hits bigint NOT NULL DEFAULT 0,
  mtproxy_raw jsonb NOT NULL DEFAULT '{}'::jsonb,
  relay_raw text NOT NULL DEFAULT ''
);
CREATE INDEX node_stats_node_time_idx ON node_stats_snapshots(node_id, taken_at DESC);

CREATE TABLE alerts (
  id bigserial PRIMARY KEY,
  node_id uuid REFERENCES nodes(id) ON DELETE CASCADE,
  kind text NOT NULL,
  message text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz
);

CREATE TABLE subscription_tokens (
  token_hash text PRIMARY KEY,
  access_key_id uuid NOT NULL REFERENCES access_keys(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz
);

CREATE TABLE settings (
  key text PRIMARY KEY,
  value jsonb NOT NULL
);

CREATE TABLE backups (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  path text NOT NULL,
  size bigint NOT NULL DEFAULT 0,
  kind backup_kind NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS backups, settings, subscription_tokens, alerts, node_stats_snapshots, audit_log,
  branding_profiles, apply_jobs, node_sites, site_templates, key_bindings, profiles, access_keys,
  nodes, sessions, recovery_codes, admin_users;
DROP TYPE IF EXISTS backup_kind, apply_kind, apply_status, key_status, key_type, sync_state, node_status, admin_role;
