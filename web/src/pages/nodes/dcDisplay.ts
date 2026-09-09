import type { DcLatency, Node, SeriesPoint } from '@/api/types';
import type { StatTone } from '@/components/common/statTone';

/** Below this a DC is quiet: the round trip is not what a user would notice. */
export const DC_WARN_MS = 150;
/** From here a DC is a fault: messages on that route visibly lag. */
export const DC_ERR_MS = 400;

/** The tones a DC figure can take. Info never applies: a latency is quiet, slow or failing. */
export type DcTone = Extract<StatTone, 'neutral' | 'ok' | 'warn' | 'err'>;

// The tone of one latency figure.
export function dcTone(latencyMs: number | null): DcTone {
  if (latencyMs === null || !Number.isFinite(latencyMs)) return 'neutral';
  if (latencyMs >= DC_ERR_MS) return 'err';
  if (latencyMs >= DC_WARN_MS) return 'warn';
  return 'ok';
}

// The tone of the route to Telegram as a whole.
export function routeTone({ healthy, fails }: { healthy: boolean; fails: number }): DcTone {
  if (!healthy) return 'err';
  if (fails > 0) return 'warn';
  return 'ok';
}

// The text class for a figure in each tone.
export const DC_TONE_TEXT: Record<DcTone, string> = {
  neutral: 'text-dim',
  ok: 'text-mute',
  warn: 'text-warn',
  err: 'text-err',
};

/** The fill class of the 7px dot beside a route line, per tone. */
export const DC_TONE_DOT: Record<DcTone, string> = {
  neutral: 'bg-dim',
  ok: 'bg-ok',
  warn: 'bg-warn',
  err: 'bg-err',
};

export function dcLatencyOf(d: Pick<DcLatency, 'latency_ms' | 'known'>): number | null {
  if (d.known === false) return null;
  if (!Number.isFinite(d.latency_ms) || d.latency_ms <= 0) return null;
  return d.latency_ms;
}

export function nodeDcLatency(node: Pick<Node, 'status' | 'health'>): number | undefined {
  const health = node.health;
  if (!health || node.status === 'offline') return undefined;
  if (health.dc_data_available === false) return undefined;
  const ms = health.effective_latency_ms;
  if (ms === undefined || !Number.isFinite(ms) || ms <= 0) return undefined;
  return ms;
}

// The fleet's latency to Telegram: the mean over the nodes that report one.
export function meanDcLatency(nodes: Array<Pick<Node, 'status' | 'health'>>): number | null {
  const figures = nodes.map(nodeDcLatency).filter((v): v is number => v !== undefined);
  if (figures.length === 0) return null;
  return figures.reduce((sum, v) => sum + v, 0) / figures.length;
}

/** The recharts data key of one DC's line. Not the bare number, which would collide with the timestamp key. */
export function dcSeriesKey(dc: number): string {
  return `dc_${dc}`;
}

/** How many `--series-N` tokens index.css defines. Telegram has five DCs, so the lines never wrap round. */
const SERIES_TOKENS = 8;

export function dcSeriesColor(i: number): string {
  return `var(--series-${(i % SERIES_TOKENS) + 1})`;
}

/** One chart row: the timestamp, then one figure per DC that was measured at that moment. */
export type DcLatencyRow = { t: string } & Record<string, number | string>;

export function dcLatencyRows(points: SeriesPoint[]): { dcs: number[]; rows: DcLatencyRow[] } {
  const seen = new Set<number>();
  const rows: DcLatencyRow[] = [];
  for (const p of points) {
    const row: DcLatencyRow = { t: p.t };
    let any = false;
    for (const [key, value] of Object.entries(p.dc_latency ?? {})) {
      const dc = Number(key);
      if (!Number.isInteger(dc) || dc <= 0 || typeof value !== 'number' || !Number.isFinite(value)) continue;
      row[dcSeriesKey(dc)] = value;
      seen.add(dc);
      any = true;
    }
    if (any) rows.push(row);
  }
  return { dcs: [...seen].sort((a, b) => a - b), rows };
}
