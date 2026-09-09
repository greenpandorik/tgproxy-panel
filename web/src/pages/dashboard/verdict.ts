import { isMetricPresent } from '@/components/common/metric';

import type { HealthStatus } from '@/components/common/healthStatus';
import type { Metric } from '@/components/common/metric';

/** What the panel knows about the fleet right now. Every figure may be missing. */
export interface FleetFacts {
  online: Metric<number>;
  total: Metric<number>;
  offline: Metric<number>;
  degraded: Metric<number>;
  /** Open alerts, absent when the alerts call did not answer. */
  attention: Metric<number>;
}

export interface Verdict {
  status: HealthStatus;
  /** i18n key of the one sentence that opens the page. */
  headline: string;
}

/** The page's opening sentence, decided from the counts the panel actually has. */
export function fleetVerdict(facts: FleetFacts): Verdict {
  const { online, total, offline, degraded, attention } = facts;

  if (!isMetricPresent(online) || !isMetricPresent(total)) {
    return { status: 'unknown', headline: 'dashboard.verdict_unknown' };
  }

  const down = isMetricPresent(offline) ? offline : Math.max(0, total - online);
  const partial = isMetricPresent(degraded) ? degraded : 0;
  if (total > 0 && online === 0) {
    return { status: 'offline', headline: 'dashboard.verdict_all_down' };
  }
  if (down > 0) {
    return { status: 'degraded', headline: 'dashboard.verdict_nodes_down' };
  }
  if (partial > 0) {
    return { status: 'degraded', headline: 'dashboard.verdict_nodes_degraded' };
  }
  if (isMetricPresent(attention) && attention > 0) {
    return { status: 'degraded', headline: 'dashboard.verdict_attention' };
  }
  if (!isMetricPresent(attention)) {
    return { status: 'unknown', headline: 'dashboard.verdict_partial' };
  }
  return { status: 'healthy', headline: 'dashboard.verdict_ok' };
}
