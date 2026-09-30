import type { TFunction, i18n as I18n } from 'i18next';

import type { Alert } from '@/api/types';

const PROBE_CHECKS = ['tls', 'http', 'faketls', 'web'] as const;

const DC = /^(dc|dc_writers)_(-?\d+)$/;

function dcTitle(id: string, t: TFunction): string {
  return id.startsWith('-') ? t('notify.title_dc_media', { dc: id.slice(1) }) : t('notify.title_dc', { dc: id });
}

function checkTitle(rest: string, t: TFunction, i18n: I18n): string | null {
  for (let i = rest.indexOf('_'); i !== -1; i = rest.indexOf('_', i + 1)) {
    const check = rest.slice(i + 1);
    const dc = DC.exec(check);
    if (dc && dc[1] === 'dc_writers') return dcTitle(dc[2], t);
    if (check.startsWith('route_')) return t('notify.title_route', { route: check.slice('route_'.length) });
    if (i18n.exists(`web.check_${check}`)) return t(`web.check_${check}`);
  }
  return null;
}

/** What an incident is about, in the interface's language rather than the server's log line. */
export function alertTitle(alert: Pick<Alert, 'kind' | 'message'>, t: TFunction, i18n: I18n): string {
  const kind = alert.kind.startsWith('reliability_') ? alert.kind.slice('reliability_'.length) : alert.kind;

  const own = `dashboard.alert_${kind}`;
  if (i18n.exists(own)) return t(own);

  const dc = DC.exec(kind);
  if (dc && dc[1] === 'dc') return dcTitle(dc[2], t);

  if (kind.startsWith('diagnostic_')) {
    const rest = kind.slice('diagnostic_'.length);
    if (rest.startsWith('public_addresses_')) return t('dashboard.alert_diagnostic_address', { address: rest.slice('public_addresses_'.length) });
    const check = checkTitle(rest, t, i18n);
    if (check) return t('dashboard.alert_diagnostic', { check });
  }

  if (kind.startsWith('probe_')) {
    const rest = kind.slice('probe_'.length);
    if (rest.endsWith('_stale')) return t('dashboard.alert_probe_stale', { location: rest.slice(0, -'_stale'.length) });
    for (const check of PROBE_CHECKS) {
      if (rest.endsWith(`_${check}`)) {
        return t('dashboard.alert_probe_failed', { location: rest.slice(0, -check.length - 1), check: t(`probe.${check}`) });
      }
    }
  }

  return alert.message || t('dashboard.alert_unknown');
}
