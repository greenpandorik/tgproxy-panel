import type { DcLatency, Node, SeriesPoint } from '@/api/types';
import type { StatTone } from '@/components/common/statTone';

/*
 * How the panel reads a node's link to Telegram's datacenters.
 *
 * telemt keeps a moving average of the latency to every DC, refreshed by its
 * own health checks, and the agent forwards it with the rest of the
 * heartbeat. Everything here is a pure reading of those figures: which tone
 * a latency takes, which tone the route takes, what one node contributes to
 * the fleet average, and how a series of snapshots becomes chart rows. The
 * thresholds are written once so the DC panel, the nodes list and the
 * dashboard tile can never disagree about what "slow" means.
 */

/** Below this a DC is quiet: the round trip is not what a user would notice. */
export const DC_WARN_MS = 150;
/** From here a DC is a fault: messages on that route visibly lag. */
export const DC_ERR_MS = 400;

/** The tones a DC figure can take. Info never applies: a latency is quiet, slow or failing. */
export type DcTone = Extract<StatTone, 'neutral' | 'ok' | 'warn' | 'err'>;

/**
 * The tone of one latency figure. Null, or anything that is not a finite
 * number, is a DC nobody has measured yet and carries no tone at all.
 */
export function dcTone(latencyMs: number | null): DcTone {
  if (latencyMs === null || !Number.isFinite(latencyMs)) return 'neutral';
  if (latencyMs >= DC_ERR_MS) return 'err';
  if (latencyMs >= DC_WARN_MS) return 'warn';
  return 'ok';
}

/**
 * The tone of the route to Telegram as a whole. An unhealthy route is a
 * fault whatever its counters say; a healthy one that has been failing is
 * worth a look; otherwise it is fine.
 */
export function routeTone({ healthy, fails }: { healthy: boolean; fails: number }): DcTone {
  if (!healthy) return 'err';
  if (fails > 0) return 'warn';
  return 'ok';
}

/**
 * The text class for a figure in each tone. Ok is --mute, not green: in a
 * row of text the panel spends colour only on what is wrong, the same rule
 * `LOAD_TONE_CLASS` follows for a load figure. Neutral is --dim because the
 * only neutral figure is the dash standing in for one nobody measured.
 */
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

/**
 * One DC's figure, or null when telemt has not measured it: it says so with
 * `known`, and before its first check completes the average is still zero,
 * which is not a round trip either.
 */
export function dcLatencyOf(d: Pick<DcLatency, 'latency_ms' | 'known'>): number | null {
  if (d.known === false) return null;
  if (!Number.isFinite(d.latency_ms) || d.latency_ms <= 0) return null;
  return d.latency_ms;
}

/**
 * The latency to Telegram one node reports, or undefined when there is
 * nothing honest to print: the node is offline (its last figure describes a
 * moment the panel cannot vouch for), it never reported, the agent said the
 * data is unavailable (a tproxy node, or telemt with upstreams disabled), an
 * older panel omitted the field, or the figure is not a measurement. Zero is
 * what telemt reports before its first check has completed, so it is not one.
 */
export function nodeDcLatency(node: Pick<Node, 'status' | 'health'>): number | undefined {
  const health = node.health;
  if (!health || node.status === 'offline') return undefined;
  if (health.dc_data_available === false) return undefined;
  const ms = health.effective_latency_ms;
  if (ms === undefined || !Number.isFinite(ms) || ms <= 0) return undefined;
  return ms;
}

/**
 * The fleet's latency to Telegram: the mean over the nodes that report one.
 * A node that reports nothing is left out rather than counted as zero, and
 * when no node reports the answer is null, not zero.
 */
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

/**
 * The colour of the i-th DC line, read from the series tokens rather than
 * the brand palette: a DC is not a node, and the tokens are the values
 * index.css has already measured against both themes.
 */
export function dcSeriesColor(i: number): string {
  return `var(--series-${(i % SERIES_TOKENS) + 1})`;
}

/** One chart row: the timestamp, then one figure per DC that was measured at that moment. */
export type DcLatencyRow = { t: string } & Record<string, number | string>;

/**
 * A node's snapshot series as chart rows, one line per DC.
 *
 * The DCs are collected across the whole window, because a DC that first
 * appeared an hour in still needs a line; they are sorted by number so the
 * legend reads DC1, DC2, DC4 whatever order the agent listed them in. A
 * point with no DC figures is dropped rather than drawn as a gap in every
 * line at once, and a key that is not a DC number is ignored.
 */
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
