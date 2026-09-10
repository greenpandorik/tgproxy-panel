
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

// Which proxy a node runs.
export type NodeEngine = 'tproxy' | 'telemt';

// One Telegram datacenter as telemt sees it.
export interface DcLatency {
  dc: number;
  latency_ms: number;
  known: boolean;
  /** Which address family telemt prefers for this DC, in telemt's own words ("ipv4", "ipv6"). */
  ip_preference: string;
}

/** telemt's carrier names, in the order the panel lists them. */
export type Carrier = 'websocket-lanes' | 'websocket' | 'https-lanes' | 'https';

export const CARRIERS: Carrier[] = ['websocket-lanes', 'websocket', 'https-lanes', 'https'];

export interface WebLearningState {
  enabled: boolean;
  entries: number;
  capacity: number;
  policy_generation: number;
  epoch: number;
  lifetime_secs: number;
  health_secs: number;
  age_ms: number;
}

export interface WebDrainState {
  operation_id: string;
  state: string;
  outcome: string;
  timeout_secs: number;
  started_epoch_millis: number;
  deadline_epoch_millis: number;
  remaining_sessions: number;
  remaining_streams: number;
  remaining_websockets: number;
  force_close_signalled: boolean;
}

export interface WebLifecycleState {
  state: string;
  epoch: number;
  age_ms: number;
  admission_open: boolean;
  effective_new_work_admission: boolean;
  drain: WebDrainState | null;
}

export interface WebCapacityResource {
  resource: string;
  unit: 'slots' | 'bytes' | 'items' | string;
  used: number;
  available: number;
  limit: number;
  closed: boolean;
}

export interface WebCapacityState {
  connection_capacity_action: 'drop' | 'wait' | 'respond' | string;
  max_http_overload_connections: number;
  http_overload_timeout_ms: number;
  resources: WebCapacityResource[];
  saturated_resources: string[];
  partial: string[];
  overload_outcomes: Record<string, number>;
}

/** The WEB runtime telemt reported. Learning and lifecycle are null when its status carried none. */
export interface WebRuntime {
  runtime_instance: string;
  carrier_negotiation: boolean;
  learning: WebLearningState | null;
  lifecycle: WebLifecycleState | null;
  capacity: WebCapacityState | null;
}

/**
 * One carrier's slice of the selection distribution. `selections` and `share` are null for a
 * carrier no node reported - absent, never a measured zero.
 */
export interface WebCarrierShare {
  carrier: string;
  selections: number | null;
  share: number | null;
}

/**
 * The WEB counters over one window. Every counter is nullable: null means telemt exposed no
 * such metric family, which is not the same as a reading of zero.
 */
export interface WebCarrierStats {
  from: string;
  to: string;
  samples: number;
  counter_resets: number;
  carrier_selections: number | null;
  carrier_failures: number | null;
  rejected_attempts: number | null;
  evicted_sessions: number | null;
  bridge_recoveries: number | null;
  learning_entries: number | null;
  carrier_selection_distribution: WebCarrierShare[];
}

/** A capability set the panel worked out for itself. A null value is one it could not settle. */
export type TelemtCapabilities = Record<string, boolean | null>;

export type CheckStatus = 'ok' | 'warn' | 'fail' | 'not_available';

export interface DiagnosticCheck {
  key: string;
  status: CheckStatus;
  value: string | null;
  detail: string | null;
}

export interface DiagnosticGroup {
  key: string;
  checks: DiagnosticCheck[];
}

/** One diagnostics pass. `passed`/`total` exclude the checks that could not run. */
export interface DiagnosticsRun {
  id: number;
  node_id: string;
  started_at: string;
  finished_at: string | null;
  overall_status: 'healthy' | 'degraded' | 'offline' | 'unknown';
  trigger: string;
  groups: DiagnosticGroup[];
  passed: number;
  total: number;
  not_run: number;
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
  /** The WEB runtime as the last heartbeat carried it. Null when the node reported none. */
  web_runtime?: WebRuntime | null;
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
  /** Sponsor-channel tag from @MTProxybot, or empty. telemt only. */
  ad_tag: string;
  telemt_version: string;
  telemt_build?: string;
  /** What the panel worked out this node's telemt can do. Null: it has not determined them yet. */
  telemt_capabilities?: TelemtCapabilities | null;
  telemt_capabilities_checked_at?: string | null;
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

export interface RegistrationSecretResult {
  secret: string;
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

// Per-key limits telemt enforces on the node.
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

// One sample of a key on one node.
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
  display_name?: string;
  category?: string;
  used_by?: number;
  screenshot_url?: string;
  variables?: Record<string, string>;
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
  // The backend serializes a nil Go slice as JSON null when there are no errors/warnings (not []).
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
  mode?: 'static' | 'upstream';
  origin?: string;
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
