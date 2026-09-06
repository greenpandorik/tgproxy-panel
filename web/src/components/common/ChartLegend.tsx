import { cn } from '@/lib/utils';

export interface LegendItem {
  key: string;
  name: string;
  color: string;
}

/**
 * Legend for the line charts, rendered outside the chart so it survives the
 * lazy recharts boundary (it shows while the chart is still loading) and so
 * the names stay real text rather than SVG.
 *
 * The swatch is a line segment, not a dot, because that is the mark it stands
 * for. Names are set in ink, never in the series colour - identity is carried
 * by the swatch beside the word, so the row still reads with colour vision
 * differences or on a black-and-white print.
 */
export function ChartLegend({ items, className }: { items: LegendItem[]; className?: string }) {
  if (items.length === 0) return null;

  return (
    <ul className={cn('mono flex flex-wrap items-center gap-x-4 gap-y-1 text-[11.5px] text-mute', className)}>
      {items.map((item) => (
        <li key={item.key} className="flex items-center gap-1.5">
          <span className="h-[2px] w-3 shrink-0 rounded-full" style={{ background: item.color }} aria-hidden="true" />
          <span className="truncate">{item.name}</span>
        </li>
      ))}
    </ul>
  );
}
