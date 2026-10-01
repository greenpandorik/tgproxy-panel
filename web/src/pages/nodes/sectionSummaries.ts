import { carrierRows, distributionIsAbsent } from '@/components/web/carrierDisplay';

import { checkTone, dcStateTone, usageTone } from './nodeHealth';

import type { CheckTally, DcState, ServiceReading } from './nodeHealth';
import type { NodeHealth, WebCarrierStats } from '@/api/types';
import type { SectionTone } from '@/components/common/CollapsibleSection';
import type { CapabilityState } from '@/components/web/capability';
import type { TFunction } from 'i18next';

/** What a closed section says about itself, and in which tone. */
export interface Summary {
  text: string;
  tone: SectionTone;
}

/** The state a live section is in before it has a reading to summarise. */
export type LiveState = 'offline' | 'error' | 'loading' | 'ready';

function notReady(state: LiveState, t: TFunction): Summary | null {
  if (state === 'offline') return { text: t('nodes.detail_summary_offline'), tone: 'neutral' };
  if (state === 'error') return { text: t('nodes.detail_summary_error'), tone: 'neutral' };
  if (state === 'loading') return { text: t('common.state.loading'), tone: 'neutral' };
  return null;
}

export function dcSummary(state: LiveState, health: NodeHealth | undefined, dcs: DcState | null, t: TFunction): Summary {
  const early = notReady(state, t);
  if (early) return early;
  if (!dcs) {
    return { text: t(health?.dc_data_available === false ? 'nodes.dcs_unavailable' : 'nodes.dcs_empty'), tone: 'neutral' };
  }
  const tone = dcStateTone(dcs);
  if (!dcs.routeHealthy) return { text: t('nodes.detail_dcs_route_down'), tone };
  if (dcs.measured < dcs.total) return { text: t('nodes.detail_dcs_unavailable', { count: dcs.total - dcs.measured }), tone };
  if (dcs.slow > 0) return { text: t('nodes.detail_dcs_slow', { count: dcs.slow }), tone };
  return { text: t('nodes.detail_dcs_ok', { count: dcs.total }), tone };
}

export function servicesSummary(
  state: LiveState,
  health: NodeHealth | undefined,
  services: ServiceReading[],
  proxyName: string,
  t: TFunction,
): Summary {
  const early = notReady(state, t);
  if (early || !health) return early ?? { text: '', tone: 'neutral' };
  const down = services.filter((s) => !s.active).map((s) => s.name);
  if (down.length > 0) return { text: t('nodes.detail_services_down', { names: down.join(', ') }), tone: 'err' };
  if (!health.healthz) return { text: t('nodes.detail_services_api_down', { name: proxyName }), tone: 'err' };
  if (!health.readyz) return { text: t('nodes.detail_services_not_ready', { name: proxyName }), tone: 'warn' };
  const disk = usageTone(health.disk_used_percent);
  if (disk === 'warn' || disk === 'err') {
    return { text: t('nodes.detail_services_disk', { value: Math.round(health.disk_used_percent) }), tone: disk };
  }
  return { text: t('nodes.detail_services_ok', { names: services.map((s) => s.name).join(', ') }), tone: 'ok' };
}

export function checksSummary(tally: CheckTally, t: TFunction): Summary {
  const tone = checkTone(tally);
  if (tally.total === 0) return { text: t('nodes.detail_checks_none'), tone };
  const problems = tally.failed + tally.warned;
  if (problems > 0) return { text: t('nodes.detail_checks_problems', { count: problems, total: tally.total }), tone };
  return { text: t('nodes.detail_checks_ok', { passed: tally.passed, total: tally.total }), tone };
}

const FAILURE_COUNTERS = ['carrier_failures', 'rejected_attempts', 'evicted_sessions'] as const;

export function webSummary(
  capability: CapabilityState,
  health: NodeHealth | undefined,
  stats: WebCarrierStats | undefined,
  t: TFunction,
): Summary {
  if (capability === 'unsupported') return { text: t('nodes.detail_web_unsupported'), tone: 'neutral' };
  if (capability === 'undetermined') return { text: t('nodes.detail_web_undetermined'), tone: 'neutral' };
  const lifecycle = health?.web_runtime?.lifecycle?.state;
  if (lifecycle === 'drained') return { text: t('web.lifecycle_state_drained'), tone: 'err' };
  if (lifecycle === 'paused' || lifecycle === 'draining' || lifecycle === 'force_closing') {
    return { text: t(`web.lifecycle_state_${lifecycle}`), tone: 'warn' };
  }
  if ((health?.web_runtime?.capacity?.saturated_resources.length ?? 0) > 0) {
    return { text: t('nodes.detail_web_saturated'), tone: 'err' };
  }
  if (!stats) return { text: '', tone: 'neutral' };
  const rows = carrierRows(stats.carrier_selection_distribution);
  if (distributionIsAbsent(rows)) return { text: t('nodes.detail_web_no_data'), tone: 'neutral' };
  const used = rows.filter((r) => (r.selections ?? 0) > 0).length;
  const failures = FAILURE_COUNTERS.map((key) => stats[key]).filter((v): v is number => typeof v === 'number');
  const parts = [t('nodes.detail_web_carriers', { count: used })];
  if (failures.length > 0) {
    const total = failures.reduce((sum, v) => sum + v, 0);
    parts.push(total > 0 ? t('nodes.detail_web_errors', { count: total }) : t('nodes.detail_web_no_errors'));
  }
  return { text: parts.join(', '), tone: 'neutral' };
}
