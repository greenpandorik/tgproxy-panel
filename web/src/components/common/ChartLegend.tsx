import { cn } from '@/lib/utils';

export interface LegendItem {
  key: string;
  name: string;
  color: string;
}

export function ChartLegend({ items, className }: { items: LegendItem[]; className?: string }) {
  if (items.length === 0) return null;

  return (
    <ul className={cn('mono flex flex-wrap items-center gap-x-4 gap-y-1 text-micro text-mute', className)}>
      {items.map((item) => (
        <li key={item.key} className="flex items-center gap-1.5">
          <span className="h-[2px] w-3 shrink-0 rounded-pill" style={{ background: item.color }} aria-hidden="true" />
          <span className="truncate">{item.name}</span>
        </li>
      ))}
    </ul>
  );
}
