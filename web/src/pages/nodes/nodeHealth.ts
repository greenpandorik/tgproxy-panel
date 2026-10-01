import { isMetricPresent } from '@/components/common/metric';
import { LOAD_ERR_PERCENT, LOAD_WARN_PERCENT } from '@/components/common/statTone';
import { tallyChecks } from '@/components/web/diagnostics';

import { DC_ERR_MS, dcLatencyOf } from './dcDisplay';

import type { ApplyStatus, DiagnosticsRun, Node, NodeCheckReport, NodeHealth } from '@/api/types';
import type { SectionTone } from '@/components/common/CollapsibleSection';

/** The tone of a usage figure: quiet below 80%, a warning to 95%, an error past it. */
export function usageTone(...percents: Array<number | null | undefined>): SectionTone {
  const known = percents.filter((p): p is number => isMetricPresent(p));
  if (known.length === 0) return 'neutral';
  const worst = Math.max(...known);
  if (worst >= LOAD_ERR_PERCENT) return 'err';
  if (worst >= LOAD_WARN_PERCENT) return 'warn';
  return 'ok';
}

/** How the server reaches Telegram's datacenters, from one health readout. */
export interface DcState {
  total: number;
  /** DCs with a measured latency. */
  measured: number;
  /** Measured DCs past the error threshold. */
  slow: number;
  routeHealthy: boolean;
  /** telemt's one latency figure for the route, or null when it has none. */
  latency: number | null;
}

export function dcState(health: NodeHealth | undefined): DcState | null {
  if (!health || health.dc_data_available === false || !Array.isArray(health.dcs) || health.dcs.length === 0) return null;
  const figures = health.dcs.map(dcLatencyOf);
  const measured = figures.filter((ms): ms is number => ms !== null);
  const latency = health.effective_latency_ms;
  return {
    total: figures.length,
    measured: measured.length,
    slow: measured.filter((ms) => ms >= DC_ERR_MS).length,
    routeHealthy: health.upstream_healthy ?? true,
    latency: isMetricPresent(latency) && latency > 0 ? latency : null,
  };
}

export function dcStateTone(state: DcState): SectionTone {
  if (!state.routeHealthy) return 'err';
  if (state.measured < state.total || state.slow > 0) return 'warn';
  return 'ok';
}

/** One service the agent reports on, by the name the server owner knows it by. */
export interface ServiceReading {
  id: string;
  name: string;
  active: boolean;
}

export function serviceReadings(node: Pick<Node, 'engine' | 'online'>, health: NodeHealth, agentName: string): ServiceReading[] {
  const proxy: ServiceReading[] =
    node.engine === 'telemt'
      ? [{ id: 'telemt', name: 'telemt', active: health.relay_active }]
      : [
          { id: 'relay', name: 'relay', active: health.relay_active },
          { id: 'mtproxy', name: 'MTProxy', active: health.mtproxy_active },
        ];
  return [
    ...proxy,
    { id: 'caddy', name: 'caddy', active: health.caddy_active },
    { id: 'agent', name: agentName, active: node.online },
  ];
}

/** Every check the server's checks section counts: the full check, the panel's own and the outside probes. */
export interface CheckTally {
  passed: number;
  warned: number;
  failed: number;
  total: number;
  /** When the full check or the panel's check last ran, whichever is newer. */
  ranAt: string | null;
}

export function checkTally(
  run: DiagnosticsRun | undefined,
  report: NodeCheckReport | null | undefined,
  probes: Array<{ status: 'ok' | 'failed' | 'not_run' }> = [],
): CheckTally {
  const full = tallyChecks(run?.groups);
  const panel = (report?.results ?? []).filter((r) => !r.advisory);
  const panelPassed = panel.filter((r) => r.ok).length;
  const probesRun = probes.filter((p) => p.status !== 'not_run');
  const probesPassed = probesRun.filter((p) => p.status === 'ok').length;
  const times = [run?.finished_at ?? run?.started_at, report?.ran_at].filter((v): v is string => !!v);
  return {
    passed: full.passed + panelPassed + probesPassed,
    warned: full.warned,
    failed: full.failed + (panel.length - panelPassed) + (probesRun.length - probesPassed),
    total: full.total + panel.length + probesRun.length,
    ranAt: times.length > 0 ? times.reduce((a, b) => (Date.parse(a) >= Date.parse(b) ? a : b)) : null,
  };
}

export function checkTone(tally: CheckTally): SectionTone {
  if (tally.total === 0) return 'neutral';
  if (tally.failed > 0) return 'err';
  if (tally.warned > 0) return 'warn';
  return 'ok';
}

/** An apply that did not leave the server as the panel wanted it. */
export function applyFailed(status: ApplyStatus): boolean {
  return status === 'failed' || status === 'rolled_back';
}
