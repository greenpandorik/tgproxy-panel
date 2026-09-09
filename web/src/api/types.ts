// Mirrors the JSON shapes produced by internal/api/*.go. Keep field names in
// snake_case exactly as the backend serializes them - no client-side renaming.

export interface Paginated<T> {
  items: T[];
  total: number;
  page?: number;
  per_page?: number;
}

// --- auth -------------------------------------------------------------

export type AdminRole = 'owner' | 'admin' | 'viewer';

export interface Me {
  id: string;
  username: string;
  role: AdminRole;
  totp_enabled: boolean;
  features: { totp: boolean };
}

// POST /auth/login answers with one of two shapes: a session (Me) when the password
// is the only factor, or a challenge when the account has TOTP enrolled. The caller
// discriminates on `totp_required`.
export interface TotpChallenge {
  totp_required: true;
  challenge: string;
}

export type LoginResult = Me | TotpChallenge;

export const isTotpChallenge = (r: LoginResult): r is TotpChallenge => 'totp_required' in r;

export interface TotpSetup {
  secret: string;
  otpauth_url: string;
  qr_data_uri: string;
}

export interface TotpEnrolment {
  recovery_codes: string[];
}

/** Second factor for verify/disable: a code from the app, or a one-time recovery code. */
export interface SecondFactor {
  code?: string;
  recovery_code?: string;
}

export interface Admin {
  id: string;
  username: string;
  role: AdminRole;
  totp_enabled?: boolean;
  created_at?: string;
}

// --- nodes --------------------------------------------------------------

export type NodeStatus = 'pending' | 'online' | 'offline' | 'degraded';

/**
 * Which proxy a node runs. `telemt` is one process serving both the WEB
 * transport and a Fake-TLS listener; `tproxy` is the original relay + MTProxy
 * pair, which only ever offers a WEB link.
 */
export type NodeEngine = 'tproxy' | 'telemt';

/**
 * One Telegram datacenter as telemt sees it. `latency_ms` is telemt's own
 * moving average over its health checks; `known` is false (and the latency
 * meaningless) for a DC it has not measured yet.
 */
export interface DcLatency {
  dc: number;
  latency_ms: number;
  known: boolean;
  /** Which address family telemt prefers for this DC, in telemt's own words ("ipv4", "ipv6"). */
  ip_preference: string;
}

export interface NodeHealth {
  relay_active: boolean;
  mtproxy_active: boolean;
  caddy_active: boolean;
  healthz: boolean;
  readyz: boolean;
  tproxy_version: string;
  agent_version: string;
  uptime_seconds: number;
  cpu_percent: number;
  mem_used_percent: number;
  disk_used_percent: number;
  profile_count: number;
  // The node's link to Telegram's datacenters, from telemt's /v1/stats/upstreams.
  // Every field is optional: a tproxy node has none of them, and a panel older
  // than the DC connectivity phase omits them entirely.
  dcs?: DcLatency[];
  upstream_healthy?: boolean;
  upstream_fails?: number;
  /** telemt's one latency figure for the route as a whole, in ms. */
  effective_latency_ms?: number;
  connect_success_total?: number;
  connect_fail_total?: number;
  upstream_last_check_age_secs?: number;
  /** False on tproxy, and when telemt reported its upstreams as disabled or the call failed. */
  dc_data_available?: boolean;
}

export interface NodeCheckResult {
  name: string;
  ok: boolean;
  detail: string;
  /** Informational probe (pq_kex): not counted in all_ok; a failure is shown in the info tone. */
  advisory?: boolean;
}

export interface NodeCheckReport {
  ran_at: string;
  results: NodeCheckResult[];
  all_ok: boolean;
}

export interface Node {
  id: string;
  name: string;
  hostname: string;
  public_ip: string;
  acme_email: string;
  status: NodeStatus;
  online: boolean;
  engine: NodeEngine;
  /** SNI the Fake-TLS listener masks behind. telemt only; defaults to the hostname. */
  tls_domain: string;
  /** Port of the Fake-TLS listener. telemt only. */
  classic_port: number;
  /** Sponsor-channel tag (32 hex chars) registered with @MTProxybot, or empty. telemt only. */
  ad_tag: string;
  telemt_version: string;
  tproxy_version: string;
  agent_version: string;
  max_profiles: number;
  profile_count: number;
  dirty: boolean;
  last_seen_at: string | null;
  last_apply_at: string | null;
  created_at: string;
  health?: NodeHealth;
  last_check: NodeCheckReport | null;
}

export interface CreateNodeInput {
  name: string;
  hostname: string;
  acme_email: string;
  public_ip?: string;
  engine?: NodeEngine;
  tls_domain?: string;
  classic_port?: number;
}

export interface PatchNodeInput {
  name?: string;
  public_ip?: string;
  max_profiles?: number;
  acme_email?: string;
  tls_domain?: string;
  classic_port?: number;
  ad_tag?: string;
}

export interface CreateNodeResult {
  node: Node;
  install_command: string;
  expires_at: string;
}

export interface InstallCommandResult {
  command: string;
  expires_at: string;
}

export type SyncState = 'in_sync' | 'pending' | 'db_only' | 'node_only' | string;

export interface Profile {
  id: string;
  name: string;
  access_key_id: string | null;
  backend: string;
  carrier_mode: CarrierMode;
  limits: ProfileLimits;
  sync_state: SyncState;
  created_at: string;
  /** Label of the key this profile serves; "" for the node's own default profile. */
  key_label: string;
  key_expires_at: string | null;
  /** The key's telemt limits, `{}` when it has none (and always for the default profile). */
  telemt_limits: TelemtLimits;
}

export interface LiveProfile {
  name: string;
  backend: string;
  carrier_mode: CarrierMode;
  limits: ProfileLimits;
}

export type ApplyStatus = 'pending' | 'running' | 'ok' | 'failed' | 'rolled_back' | string;
export type ApplyKind = 'full' | 'incremental' | string;

export interface ApplyJob {
  id: string;
  node_id: string;
  node_name?: string;
  status: ApplyStatus;
  kind: ApplyKind;
  started_at: string | null;
  finished_at: string | null;
  error: string;
  log?: string;
  created_at: string;
}

export interface NodeStats {
  [key: string]: string;
}

export interface LogLine {
  service: string;
  line: string;
  time: string;
}

// --- keys -----------------------------------------------------------------

export type KeyType = 'SHARED' | 'PERSONAL';
export type KeyStatus = 'pending' | 'active' | 'revoked';
export type CarrierMode = 'https' | 'https-lanes' | 'websocket' | 'websocket-lanes';

export interface ProfileLimits {
  max_sessions?: number;
  max_streams?: number;
  max_backend_dials_in_flight?: number;
  new_sessions_per_minute?: number;
  new_sessions_burst?: number;
  new_streams_per_minute?: number;
  new_streams_burst?: number;
  max_streams_per_session?: number;
  max_pending_per_session?: number;
}

/**
 * Per-key limits telemt enforces on the node. Every field is optional and 0/absent
 * means "no limit" - the backend omits zero fields entirely (omitempty), so a key
 * with no limits arrives as `{}`.
 */
export interface TelemtLimits {
  data_quota_bytes?: number;
  rate_limit_up_bps?: number;
  rate_limit_down_bps?: number;
  max_unique_ips?: number;
  max_tcp_conns?: number;
}

export interface KeyNode {
  node_id: string;
  node_name: string;
  hostname: string;
  profile_sync: SyncState;
}

/**
 * A link kind: `web` is the WEB-transport link every node offers, `tls` the
 * Fake-TLS (classic MTProto) link only telemt nodes listen for.
 */
export type LinkKind = 'web' | 'tls';

export interface Link {
  node_id: string;
  node_name: string;
  hostname: string;
  kind: LinkKind;
  tme: string;
  tg: string;
}

/** One link form of one kind, inside a node's group. */
export interface KindLink {
  kind: LinkKind;
  tme: string;
  tg: string;
}

/** Every link a key has on one node - what GET /keys/{id}/links returns per node. */
export interface NodeLinkGroup {
  node_id: string;
  node_name: string;
  hostname: string;
  engine: NodeEngine;
  links: KindLink[];
}

export interface KeyLinksResult {
  items: NodeLinkGroup[];
  client_support: ClientSupport;
}

export type ClientSupport = Record<'desktop' | 'android' | 'ios', string>;

export interface AccessKey {
  id: string;
  label: string;
  type: KeyType;
  owner_label: string;
  status: KeyStatus;
  carrier_mode: CarrierMode;
  limits: ProfileLimits;
  telemt_limits: TelemtLimits;
  expires_at: string | null;
  revoked_at: string | null;
  note: string;
  created_at: string;
  nodes: KeyNode[];
  secret?: string;
  links?: Link[];
  client_support: ClientSupport;
  subscription_active: boolean;
  /** Octets the key moved across its telemt nodes in the last 30 days; 0 on tproxy-only keys. */
  traffic_30d: number;
}

export interface SubscriptionCreated {
  url: string;
  qr_data_uri: string;
}

export interface KeyInput {
  label: string;
  type: KeyType;
  owner_label?: string;
  note?: string;
  carrier_mode?: CarrierMode;
  limits?: ProfileLimits;
  telemt_limits?: TelemtLimits;
  expires_at?: string | null;
  node_ids: string[];
  prefix?: string;
  count?: number;
}

export interface PatchKeyInput {
  label?: string;
  owner_label?: string;
  note?: string;
  carrier_mode?: CarrierMode;
  limits?: ProfileLimits;
  telemt_limits?: TelemtLimits;
  expires_at?: string | null;
  clear_expiry?: boolean;
}

export interface KeyFilters {
  page?: number;
  per_page?: number;
  type?: KeyType;
  status?: KeyStatus;
  node?: string;
  q?: string;
}

export type BulkKeyAction = 'revoke' | 'delete' | 'extend';

export interface BulkKeysInput {
  action: BulkKeyAction;
  ids: string[];
  expires_at?: string;
}

export interface BulkKeysResult {
  done: number;
  failed: Record<string, string>;
}

/**
 * One sample of a key on one node. `total_octets` is telemt's cumulative counter,
 * so what a reader wants - traffic in an interval - is the difference between two
 * samples, and a restart of telemt resets it to zero.
 */
export interface KeyStatsPoint {
  t: string;
  connections: number;
  total_octets: number;
}

export interface KeyStatsNode {
  node_id: string;
  node_name: string;
  points: KeyStatsPoint[];
}

export interface KeyStats {
  nodes: KeyStatsNode[];
  totals: {
    connections_now: number;
    /** Traffic in the requested window: last sample minus first, per node, summed. */
    octets_delta: number;
  };
}

// --- site templates ---------------------------------------------------

export interface SiteTemplate {
  id: string;
  name: string;
  is_preset: boolean;
  created_at: string;
  updated_at: string;
  html?: string;
  assets?: Record<string, string>;
}

export interface TemplateInput {
  name: string;
  html: string;
  assets: Record<string, string>;
}

export interface ValidateReport {
  // The backend serializes a nil Go slice as JSON null when there are no
  // errors/warnings (not []) - callers must normalize before using .length/.map.
  errors: string[] | null;
  warnings: string[] | null;
}

export interface ValidateResult {
  report: ValidateReport;
  files: string[];
}

export interface NodeSite {
  template_id: string | null;
  bundle_hash: string;
  deployed_hash: string | null;
  files: string[];
  updated_at?: string;
}

// --- branding -----------------------------------------------------------

export interface Branding {
  panel_name: string;
  logo_url: string;
  logo_dark_url?: string;
  favicon_url: string;
  primary_color: string;
  accent_color: string;
  theme_default: 'dark' | 'light';
  login_bg_url: string;
  login_text: string;
  support_link: string;
  footer_text: string;
  custom_css: string;
}

export interface BrandingProfile extends Branding {
  id: string;
  name: string;
  is_active: boolean;
  updated_at: string;
  css_removed?: string[];
}

export interface BrandingProfileInput {
  name: string;
  panel_name: string;
  primary_color: string;
  accent_color: string;
  theme_default: 'dark' | 'light';
  login_text: string;
  support_link: string;
  footer_text: string;
  custom_css: string;
}

export type BrandingAssetKind = 'logo' | 'logo_dark' | 'favicon' | 'login_bg';

// --- dashboard ------------------------------------------------------------

export interface NodeCounts {
  pending: number;
  online: number;
  offline: number;
  degraded: number;
  total: number;
}

export interface KeyCounts {
  pending: number;
  active: number;
  revoked: number;
  total: number;
}

export interface Alert {
  id: number;
  node_id?: string;
  node_name?: string;
  kind: string;
  message: string;
  created_at: string;
}

export interface DashboardSummary {
  nodes: NodeCounts;
  keys: KeyCounts;
  sessions_live: number;
  streams_live: number;
  bytes_up: number;
  bytes_down: number;
  alerts: Alert[];
  recent_jobs: ApplyJob[];
}

/** Server load at one sample, as percentages of the node's CPU, memory and site disk. */
export interface LoadPoint {
  t: string;
  cpu_percent: number;
  mem_used_percent: number;
  disk_used_percent: number;
}

export interface SeriesPoint extends LoadPoint {
  sessions_live: number;
  streams_live: number;
  bytes_up: number;
  bytes_down: number;
  /** Latency to each Telegram DC at this sample, keyed by DC number ("1": 197.9). Absent on an older panel. */
  dc_latency?: Record<string, number>;
}

// --- monitoring -----------------------------------------------------------

export interface MonitoringNode {
  node_id: string;
  node_name: string;
  hostname: string;
  status: NodeStatus;
}

export interface MonitoringPoint extends LoadPoint {
  sessions_live: number;
  streams_live: number;
  bytes_up_rate: number;
  bytes_down_rate: number;
  /** Mean latency to each Telegram DC over the bucket, keyed by DC number. Absent on an older panel. */
  dc_latency?: Record<string, number>;
}

export interface MonitoringOverview {
  nodes: MonitoringNode[];
  series: Record<string, MonitoringPoint[]>;
}

// --- settings ---------------------------------------------------------

export interface TelegramAlertsSettings {
  enabled: boolean;
  bot_token_set: boolean;
  chat_id: string;
}

/** Nightly database dump: off, or at a fixed UTC hour keeping the newest `keep` files. */
export interface BackupSchedule {
  enabled: boolean;
  /** UTC hour, 0..23 - the panel and its nodes may sit in different zones. */
  hour: number;
  /** How many scheduled dumps to keep, 1..60. */
  keep: number;
}

export interface Settings {
  apply_interval: number;
  offline_after: number;
  telegram_alerts: TelegramAlertsSettings;
  backup_schedule: BackupSchedule;
}

export interface PutSettingsInput {
  apply_interval?: number;
  offline_after?: number;
  telegram_alerts?: { enabled: boolean; bot_token?: string | null; chat_id: string };
  backup_schedule?: BackupSchedule;
}

export interface TelegramTestInput {
  bot_token?: string;
  /** Overrides the stored chat id for this send only; never persisted. */
  chat_id?: string;
}

export interface TelegramTestResult {
  ok: boolean;
}

// --- backups ---------------------------------------------------------------

export interface Backup {
  id: string;
  /** Dump file name; the directory is the panel's own DATA_DIR/backups. */
  name: string;
  size: number;
  kind: 'manual' | 'scheduled';
  created_at: string;
}

// --- audit ---------------------------------------------------------------

export interface AuditEntry {
  id: number;
  username?: string;
  action: string;
  target_type: string;
  target_id: string;
  meta: unknown;
  ip: string;
  created_at: string;
}

export interface AuditFilters {
  page?: number;
  per_page?: number;
  action?: string;
  user?: string;
  from?: string;
  to?: string;
}
