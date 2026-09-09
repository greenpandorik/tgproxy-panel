import type { Status } from '@/components/common/StatusBadge';
import type { Node } from '@/api/types';

/** What the panel prints where a machine reported nothing. */
export const DASH = '—';

/** How an uncapped node's capacity is written. */
export const INFINITY = '∞';

export function nodeStatus(node: Node): Status {
  return node.status;
}

// Profile capacity as text.
export function capacityText(count: number, max: number): string {
  return `${count} / ${max > 0 ? max : INFINITY}`;
}

/** Short display form of a version string: first 8 chars for hex commit hashes, the raw value otherwise, "—" if empty. */
export function shortVersion(v: string): string {
  if (!v) return DASH;
  if (/^[0-9a-f]{20,}$/i.test(v)) return v.slice(0, 8);
  return v.length > 16 ? v.slice(0, 16) + '…' : v;
}

// The telemt build a node reports.
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

export function nodeLoad(node: Pick<Node, 'status' | 'health'>): { cpu: number; mem: number } | undefined {
  if (!node.health || node.status === 'offline') return undefined;
  const clamp = (v: number) => Math.max(0, Math.min(100, Number.isFinite(v) ? v : 0));
  return { cpu: clamp(node.health.cpu_percent), mem: clamp(node.health.mem_used_percent) };
}
