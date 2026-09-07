/*
 * Shared chrome for every recharts chart in the panel.
 *
 * Deliberately free of any recharts import: this module is pulled in by the
 * lazily loaded chart components, and keeping it library-agnostic means the
 * axis/grid/tooltip decisions live in one place instead of being retyped -
 * and drifting - in each chart.
 *
 * The chrome recedes on purpose. Grid lines are the same hairline that
 * separates table rows, axis lines and tick marks are gone entirely, and tick
 * labels are 10px mono in --dim, so the only thing carrying ink weight in the
 * plot area is the data.
 */

/** Applied to the chart wrapper: every SVG label inside becomes mono/tabular in --dim. */
export const chartTextClass =
  'w-full [&_.recharts-cartesian-axis-tick_text]:fill-dim [&_svg_text]:font-mono [&_svg_text]:tabular-nums';

export const gridProps = {
  stroke: 'var(--line)',
  strokeWidth: 1,
  vertical: false,
} as const;

// Axis values are machine output, so they take the mono role's size from the
// type scale rather than a number of their own. Recharts renders SVG text and
// cannot take a Tailwind class here, so the token is read directly.
const tick = { fontSize: 'var(--t-mono)' } as const;

export const xAxisProps = {
  dataKey: 't',
  tick,
  tickLine: false,
  axisLine: false,
  minTickGap: 64,
  tickMargin: 8,
  height: 22,
} as const;

export const yAxisProps = {
  tick,
  tickLine: false,
  axisLine: false,
  tickMargin: 6,
} as const;

/** Hour:minute in the operator's locale - the axis of a 24h window needs nothing more. */
export function formatTimeTick(value: string, locale: string): string {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '';
  return new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit' }).format(d);
}

/**
 * A tick formatter for a byte axis, locked to one unit.
 *
 * `formatBytes` picks a unit per value, which down an axis produces "3 GB",
 * "668 MB", "0 B" - three scales in one column, so the ticks stop being
 * comparable at a glance. The unit is chosen once from the axis maximum and
 * every tick is printed in it. One decimal below 10, none above, because the
 * axis is read for magnitude, not for precision.
 */
export function bytesAxisFormatter(max: number): (value: number) => string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'] as const;
  const exp = max > 0 ? Math.min(Math.floor(Math.log(max) / Math.log(1024)), units.length - 1) : 0;
  const unit = units[exp];
  return (value: number) => {
    if (!Number.isFinite(value) || value <= 0) return `0 ${unit}`;
    const scaled = value / 1024 ** exp;
    return `${scaled.toFixed(exp === 0 || scaled >= 10 ? 0 : 1)} ${unit}`;
  };
}
