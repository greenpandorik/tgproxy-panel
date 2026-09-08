/**
 * What tone a stat tile paints itself in, decided by the fact it is showing.
 *
 * Eleven tiles of the same size do not tell an operator what to read first,
 * and the fix is not to make one of them bigger. It is that a tile whose fact
 * means a problem carries its tone only while the problem is there: nodes
 * offline `0` is a calm neutral tile, nodes offline `1` is a red one. So
 * "look here" is said by the state of the system rather than by the layout,
 * and a healthy dashboard is almost entirely neutral with one green tile on
 * it saying the fleet is up.
 *
 * Facts with no state - how many sessions there are, how many bytes moved,
 * how many keys are live - are never a problem or a success, so they are
 * always neutral. Colour in this panel is meaning, and a number that cannot
 * be good or bad has no meaning to spend it on.
 *
 * The whole rule lives here, in one pure function, rather than as a tone
 * prop guessed at each of the eleven call sites.
 */
export type StatTone = 'neutral' | 'ok' | 'warn' | 'err' | 'info';

/**
 * Where average CPU load stops being quiet. Same two thresholds as
 * `loadTone` in pages/nodes/nodeDisplay.ts, which paints the per-node figure
 * in the tables; the tile is an average of the same readings and must not
 * disagree with the column it summarises.
 */
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
  | { kind: 'avg_load'; percent: number | null };

export function statTone(fact: TileFact): StatTone {
  switch (fact.kind) {
    case 'nodes_online':
      // A fleet with no nodes in it is not a fleet that is up: the dashboard
      // shows its empty state long before this, so total 0 stays neutral.
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
    case 'stateless':
    default:
      return 'neutral';
  }
}
