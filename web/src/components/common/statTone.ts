import { dcTone } from '@/pages/nodes/dcDisplay';

export type StatTone = 'neutral' | 'ok' | 'warn' | 'err' | 'info';

/** The tone tokens, as the custom property `.tgwp-tone-tint` reads. */
export const TONE_VAR: Record<StatTone, string> = {
  neutral: 'var(--mute)',
  ok: 'var(--status-ok)',
  warn: 'var(--status-warn)',
  err: 'var(--status-err)',
  info: 'var(--status-info)',
};

// Where average CPU load stops being quiet.
export const LOAD_WARN_PERCENT = 80;
export const LOAD_ERR_PERCENT = 95;

/** The fact a tile is showing, in the only terms that decide its tone. */
export type TileFact =
  /** Sessions, streams, traffic, active keys: a number that is neither good nor bad. */
  | { kind: 'stateless' }
  /** Green while the whole fleet is reporting, amber the moment it is not. */
  | { kind: 'nodes_online'; online: number; total: number }
  /** A count of nodes that stopped reporting. Any of them is a fault. */
  | { kind: 'nodes_offline'; count: number }
  /** A count of nodes answering only partly. Not down, so amber and not red. */
  | { kind: 'degraded'; count: number }
  /** Open alerts. Something is waiting for a person; amber until it is not. */
  | { kind: 'alerts_open'; count: number }
  /** Keys waiting for the next push. Not a fault, but not nothing either. */
  | { kind: 'keys_pending'; count: number }
  /** Average CPU across the online nodes, or null when nothing is reporting. */
  | { kind: 'avg_load'; percent: number | null }
  /** Mean latency to Telegram across the nodes that report one, or null when none does. */
  | { kind: 'dc_latency'; ms: number | null };

export function statTone(fact: TileFact): StatTone {
  switch (fact.kind) {
    case 'nodes_online':
      if (fact.total <= 0) return 'neutral';
      return fact.online >= fact.total ? 'ok' : 'warn';
    case 'nodes_offline':
      return fact.count > 0 ? 'err' : 'neutral';
    case 'degraded':
      return fact.count > 0 ? 'warn' : 'neutral';
    case 'alerts_open':
      return fact.count > 0 ? 'warn' : 'neutral';
    case 'keys_pending':
      return fact.count > 0 ? 'info' : 'neutral';
    case 'avg_load':
      if (fact.percent === null || !Number.isFinite(fact.percent)) return 'neutral';
      if (fact.percent >= LOAD_ERR_PERCENT) return 'err';
      if (fact.percent >= LOAD_WARN_PERCENT) return 'warn';
      return 'ok';
    case 'dc_latency':
      return dcTone(fact.ms);
    case 'stateless':
    default:
      return 'neutral';
  }
}
