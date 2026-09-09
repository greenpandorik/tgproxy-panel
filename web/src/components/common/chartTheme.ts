
/** Applied to the chart wrapper: every SVG label inside becomes mono/tabular in --dim. */
export const chartTextClass =
  'w-full [&_.recharts-cartesian-axis-tick_text]:fill-dim [&_svg_text]:font-mono [&_svg_text]:tabular-nums';

export const gridProps = {
  stroke: 'var(--line)',
  strokeWidth: 1,
  vertical: false,
} as const;

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

// A tick formatter for a byte axis, locked to one unit.
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
