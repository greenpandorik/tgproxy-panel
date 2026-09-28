import type { TFunction, i18n as I18n } from 'i18next';

import type { Alert } from '@/api/types';

const PROBE_CHECKS = ['tls', 'http', 'faketls', 'web'] as const;

/** What an incident is about, in the interface's language rather than the server's log line. */
export function alertTitle(alert: Pick<Alert, 'kind' | 'message'>, t: TFunction, i18n: I18n): string {
  const own = `dashboard.alert_${alert.kind}`;
  if (i18n.exists(own)) return t(own);

  if (alert.kind.startsWith('diagnostic_')) {
    const rest = alert.kind.slice('diagnostic_'.length);
    if (rest.startsWith('public_addresses_')) return t('dashboard.alert_diagnostic_address', { address: rest.slice('public_addresses_'.length) });
    for (let i = rest.indexOf('_'); i !== -1; i = rest.indexOf('_', i + 1)) {
      const check = `web.check_${rest.slice(i + 1)}`;
      if (i18n.exists(check)) return t('dashboard.alert_diagnostic', { check: t(check) });
    }
  }

  if (alert.kind.startsWith('probe_')) {
    const rest = alert.kind.slice('probe_'.length);
    if (rest.endsWith('_stale')) return t('dashboard.alert_probe_stale', { location: rest.slice(0, -'_stale'.length) });
    for (const check of PROBE_CHECKS) {
      if (rest.endsWith(`_${check}`)) {
        return t('dashboard.alert_probe_failed', { location: rest.slice(0, -check.length - 1), check: t(`probe.${check}`) });
      }
    }
  }

  return alert.message || t('dashboard.alert_unknown');
}
