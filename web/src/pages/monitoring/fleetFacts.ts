import { isMetricPresent } from '@/components/common/metric';
import { dcLatencyOf } from '@/pages/nodes/dcDisplay';

import type { MonitoringNode, MonitoringPoint, Node } from '@/api/types';

const DAY_MS = 86_400_000;

/** The slowest Telegram DC any reachable server reports, with the server and the DC. */
export function worstRoute(nodes: Node[]): { ms: number; node: string; dc: number } | null {
  let worst: { ms: number; node: string; dc: number } | null = null;
  for (const node of nodes) {
    if (node.status === 'offline' || !node.health || node.health.dc_data_available === false) continue;
    for (const dc of node.health.dcs ?? []) {
      const ms = dcLatencyOf(dc);
      if (ms !== null && (!worst || ms > worst.ms)) worst = { ms, node: node.name, dc: dc.dc };
    }
  }
  return worst;
}

/** Whole days until a moment, negative once it has passed. */
export function daysLeft(at: string, now = Date.now()): number {
  return Math.floor((new Date(at).getTime() - now) / DAY_MS);
}

/** The certificate that expires first, among the servers the panel has read one for. */
export function nearestCertificate(nodes: MonitoringNode[], now = Date.now()): { days: number; node: string } | null {
  let nearest: { days: number; node: string } | null = null;
  for (const node of nodes) {
    if (!node.cert_expires_at) continue;
    const days = daysLeft(node.cert_expires_at, now);
    if (!nearest || days < nearest.days) nearest = { days, node: node.node_name };
  }
  return nearest;
}

/** Traffic per second across the servers whose last sample carries a rate, or null when none does. */
export function currentThroughput(series: Record<string, MonitoringPoint[]>): number | null {
  let total: number | null = null;
  for (const points of Object.values(series)) {
    const last = points.at(-1);
    if (last && isMetricPresent(last.bytes_up_rate) && isMetricPresent(last.bytes_down_rate)) {
      total = (total ?? 0) + last.bytes_up_rate + last.bytes_down_rate;
    }
  }
  return total;
}

/** Puts monitoring entries in the order of the servers list; any it does not know go last. */
export function inServerOrder(entries: MonitoringNode[], order: { id: string }[]): MonitoringNode[] {
  const rank = new Map(order.map((n, i) => [n.id, i]));
  return [...entries].sort((a, b) => (rank.get(a.node_id) ?? order.length) - (rank.get(b.node_id) ?? order.length));
}
