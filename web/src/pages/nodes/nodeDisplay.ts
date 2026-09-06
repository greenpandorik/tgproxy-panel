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
