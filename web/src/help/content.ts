
/** Which section of docs/setup.*.md a topic links to. Anchors are GitHub's heading slugs. */
export const DOC_SECTIONS = {
  install: { ru: '6-подключение-ноды', en: '6-adding-a-node' },
  engine: { ru: '7-движок-ноды-telemt-или-tproxy', en: '7-node-engine-telemt-or-tproxy' },
  keys: { ru: '8-выдача-ключей', en: '8-issuing-keys' },
  site: { ru: '9-сайт-заглушка-ноды', en: '9-node-cover-site' },
  monitoring: { ru: '10-мониторинг-алерты-и-аудит', en: '10-monitoring-alerts-and-audit' },
  branding: { ru: '11-брендинг', en: '11-branding' },
  backups: { ru: '12-резервные-копии-ротация-ключа-обновление', en: '12-backups-key-rotation-upgrades' },
  security: { ru: '5-первый-вход-и-двухфакторная-защита', en: '5-first-login-and-two-factor-authentication' },
  troubleshooting: { ru: '14-если-что-то-не-работает', en: '14-troubleshooting' },
} as const;

export type DocSection = keyof typeof DOC_SECTIONS;

export interface HelpTopicDef {
  /** Field ids, in the order the sheet lists them. Each needs `name` and `what` in i18n. */
  fields: readonly string[];
  /** Setup-guide section for the "Read more" link; omitted when no section fits. */
  docs?: DocSection;
}

export const HELP_TOPICS = {
  dashboard: {
    fields: ['nodes_online', 'keys_active', 'sessions', 'traffic', 'chart', 'alerts', 'jobs'],
    docs: 'monitoring',
  },

  'nodes.list': {
    fields: ['status', 'engine', 'relay', 'profiles', 'load', 'telegram', 'heartbeat', 'changes', 'actions'],
    docs: 'install',
  },
  'nodes.create': {
    fields: ['name', 'hostname', 'engine', 'tls_domain', 'classic_port', 'acme_email', 'public_ip'],
    docs: 'install',
  },
  'nodes.install': {
    fields: ['command', 'expires', 'preflight', 'installs', 'regenerate'],
    docs: 'install',
  },
  'nodes.check': {
    fields: ['dns_a', 'tcp_80', 'tcp_443', 'tls_cert', 'pq_kex', 'http_root', 'mask'],
    docs: 'install',
  },
  'nodes.detail': {
    fields: [
      'header',
      'apply',
      'restart',
      'install',
      'delete',
      'tab_overview',
      'tab_profiles',
      'tab_logs',
      'tab_site',
      'tab_stats',
    ],
    docs: 'engine',
  },
  'nodes.listeners': {
    fields: ['tls_domain', 'classic_port', 'public_ip'],
    docs: 'engine',
  },
  'nodes.dcs': {
    fields: ['route', 'connections', 'latency', 'ip_preference'],
    docs: 'monitoring',
  },

  'keys.list': {
    fields: ['type', 'status', 'nodes', 'traffic', 'expires', 'filters', 'row_actions', 'bulk'],
    docs: 'keys',
  },
  'keys.create': {
    fields: ['label', 'owner_label', 'nodes', 'carrier_mode', 'expires_at', 'limits', 'note'],
    docs: 'keys',
  },
  'keys.batch': {
    fields: ['prefix', 'count', 'result'],
    docs: 'keys',
  },
  'keys.limits': {
    fields: [
      'quota_gb',
      'rate_up_mbit',
      'rate_down_mbit',
      'max_unique_ips',
      'max_tcp_conns',
      'max_sessions',
      'max_streams',
      'max_streams_per_session',
      'max_pending_per_session',
      'max_backend_dials_in_flight',
      'new_sessions_per_minute',
      'new_sessions_burst',
      'new_streams_per_minute',
      'new_streams_burst',
    ],
    docs: 'engine',
  },
  'keys.detail': {
    fields: ['status', 'edit', 'nodes', 'link', 'subscription', 'rotate', 'revoke', 'delete'],
    docs: 'keys',
  },
  'keys.link': {
    fields: ['web', 'fake_tls', 'tme_vs_tg', 'qr', 'clients'],
    docs: 'keys',
  },
  'keys.extend': {
    fields: ['expires_at'],
    docs: 'keys',
  },

  'sites.templates': {
    fields: ['template', 'preset', 'duplicate', 'delete', 'assign'],
    docs: 'site',
  },
  'sites.editor': {
    fields: ['name', 'html', 'assets', 'validate', 'preview', 'save'],
    docs: 'site',
  },
  'sites.assign': {
    fields: ['node'],
    docs: 'site',
  },

  monitoring: {
    fields: ['range', 'sessions', 'streams', 'traffic', 'connections', 'prometheus'],
    docs: 'monitoring',
  },
  audit: {
    fields: ['action', 'user', 'dates', 'target', 'ip', 'details'],
    docs: 'monitoring',
  },

  'settings.panel': {
    fields: ['apply_interval', 'offline_after'],
    docs: 'monitoring',
  },
  'settings.telegram': {
    fields: ['enabled', 'bot_token', 'chat_id', 'test'],
    docs: 'monitoring',
  },
  'settings.security': {
    fields: ['password', 'totp', 'recovery', 'disable'],
    docs: 'security',
  },
  'settings.admins': {
    fields: ['username', 'password', 'role'],
    docs: 'security',
  },
  'settings.branding': {
    fields: [
      'personal',
      'profiles',
      'panel_name',
      'logo',
      'logo_dark',
      'favicon',
      'login_bg',
      'primary_color',
      'accent_color',
      'theme_default',
      'login_text',
      'support_link',
      'footer_text',
      'custom_css',
    ],
    docs: 'branding',
  },
  'settings.backups': {
    fields: ['create', 'download', 'delete', 'schedule_enabled', 'schedule_hour', 'schedule_keep', 'restore'],
    docs: 'backups',
  },
} as const satisfies Record<string, HelpTopicDef>;

export type HelpTopic = keyof typeof HELP_TOPICS;

export const HELP_TOPIC_IDS = Object.keys(HELP_TOPICS) as HelpTopic[];

export function isHelpTopic(id: string): id is HelpTopic {
  return Object.prototype.hasOwnProperty.call(HELP_TOPICS, id);
}

const DOCS_BASE = 'https://github.com/greenpandorik/tgproxy-panel/blob/main/docs';

/** URL of the setup-guide section for a topic in the given language, or undefined when it has none. */
export function docsUrl(topic: HelpTopic, lang: string): string | undefined {
  const section = HELP_TOPICS[topic].docs;
  if (!section) return undefined;
  const isEn = lang.startsWith('en');
  const anchor = DOC_SECTIONS[section][isEn ? 'en' : 'ru'];
  return `${DOCS_BASE}/setup.${isEn ? 'en' : 'ru'}.md#${anchor}`;
}
