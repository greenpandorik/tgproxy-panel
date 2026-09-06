import type { Status } from '@/components/common/StatusBadge';
import type { Node } from '@/api/types';

/** What the panel prints where a machine reported nothing. */
export const DASH = '—';

/** How an uncapped node's capacity is written. */
export const INFINITY = '∞';

/**
 * A node's state as the whole panel reports it: the lifecycle status the
 * heartbeat persisted, and nothing else.
 *
 * There are two candidate sources and they disagree. `status` is what the
 * panel recorded the last time the node reported in; `online` is a driver
 * probe taken while this one request was being served. Showing the probe made
 * the same node read "online" in the list and "0 online" in the rail, because
 * the rail counts persisted statuses - so the panel contradicted itself on
 * screen. One source wins everywhere a state is *displayed* (rail count,
 * lists, detail header, palette), and it is `status`.
 *
 * `online` keeps its real job: gating the live fetches (logs, health, stats)
 * that only work while the driver can actually reach the box.
 */
export function nodeStatus(node: Node): Status {
  return node.status;
}

/**
 * Profile capacity as text. `max_profiles = 0` means "no cap configured", so
 * it is written `4 / ∞` rather than the nonsense `4 / 0`.
 */
export function capacityText(count: number, max: number): string {
  return `${count} / ${max > 0 ? max : INFINITY}`;
}

/** Short display form of a version string: first 8 chars for hex commit hashes, the raw value otherwise, "—" if empty. */
export function shortVersion(v: string): string {
  if (!v) return DASH;
  if (/^[0-9a-f]{20,}$/i.test(v)) return v.slice(0, 8);
  return v.length > 16 ? v.slice(0, 16) + '…' : v;
}

/**
 * The telemt build a node reports.
 *
 * There are two places it can come from and they fill in at different times:
 * `telemt_version` is what the panel recorded for the node, while the health
 * report carries the live figure in `tproxy_version` as "telemt 3.5.5" (one
 * field for "whatever proxy this engine runs"). The recorded value wins when it
 * exists; otherwise the prefix is stripped off the live one. Empty means the
 * node has not reported yet.
 */
export function telemtVersion(node: Pick<Node, 'telemt_version' | 'tproxy_version'>): string {
  if (node.telemt_version) return node.telemt_version;
  const reported = node.tproxy_version.trim();
  if (reported.toLowerCase().startsWith('telemt ')) return reported.slice('telemt '.length).trim();
  return '';
}

/** The proxy build to print for a node, whichever engine it runs. */
export function engineVersion(node: Pick<Node, 'engine' | 'telemt_version' | 'tproxy_version'>): string {
  return node.engine === 'telemt' ? telemtVersion(node) : node.tproxy_version;
}

/** `hostname:port` of a telemt node's Fake-TLS listener, or "" when it has no domain yet. */
export function fakeTlsEndpoint(node: Pick<Node, 'engine' | 'tls_domain' | 'classic_port'>): string {
  if (node.engine !== 'telemt' || !node.tls_domain) return '';
  return `${node.tls_domain}:${node.classic_port}`;
}

/** How loud a load figure is drawn: quiet until 80%, warning to 95%, danger past it. */
export type LoadTone = 'ok' | 'warn' | 'danger';

export function loadTone(percent: number): LoadTone {
  if (percent >= 95) return 'danger';
  if (percent >= 80) return 'warn';
  return 'ok';
}

/** The text and fill classes for each tone, so a list and a table paint the same figure the same way. */
export const LOAD_TONE_CLASS: Record<LoadTone, { text: string; bar: string }> = {
  ok: { text: 'text-mute', bar: 'bg-brand-primary' },
  warn: { text: 'text-warn', bar: 'bg-warn' },
  danger: { text: 'text-err', bar: 'bg-err' },
};

/**
 * CPU and memory use from the node's last heartbeat, clamped to 0-100.
 *
 * Undefined when there is nothing honest to show: a node that has never
 * reported has no figure, and an offline node's last figure describes a
 * moment the panel can no longer vouch for, so both print as "—" rather than
 * as a number that reads as current.
 */
export function nodeLoad(node: Pick<Node, 'status' | 'health'>): { cpu: number; mem: number } | undefined {
  if (!node.health || node.status === 'offline') return undefined;
  const clamp = (v: number) => Math.max(0, Math.min(100, Number.isFinite(v) ? v : 0));
  return { cpu: clamp(node.health.cpu_percent), mem: clamp(node.health.mem_used_percent) };
}
