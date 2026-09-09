import { formatTimeTick } from './chartTheme';

export interface TooltipEntry {
  dataKey?: string | number;
  name?: string;
  value?: number | string;
  color?: string;
}

export interface ChartTooltipProps {
  active?: boolean;
  payload?: TooltipEntry[];
  label?: string;
  locale: string;
  /** Turns the raw number into what it means - a count, a byte rate. */
  formatValue?: (value: number) => string;
}

// Hover readout: the timestamp in dim mono, then one row per series with its line swatch, name and value.
export function ChartTooltip({ active, payload, label, locale, formatValue }: ChartTooltipProps) {
  if (!active || !payload || payload.length === 0) return null;

  return (
    <div className="rounded-surface border border-hairline-strong bg-card px-2.5 py-2 shadow-popover">
      <p className="mono mb-1.5 text-micro text-mute">{formatTimeTick(label ?? '', locale)}</p>
      <ul className="space-y-1">
        {payload.map((entry) => (
          <li key={String(entry.dataKey)} className="flex items-center gap-2 text-label">
            <span className="h-[2px] w-3 shrink-0 rounded-pill" style={{ background: entry.color }} aria-hidden="true" />
            <span className="text-foreground">{entry.name}</span>
            <span className="mono ml-auto pl-3 text-foreground">
              {formatValue ? formatValue(Number(entry.value ?? 0)) : String(entry.value ?? '')}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
