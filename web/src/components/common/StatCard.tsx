import { Badge } from '@/components/ui/badge';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

export type DeltaTone = 'ok' | 'warn' | 'err' | 'neutral';

const DELTA_CLASS: Record<DeltaTone, string> = {
  ok: 'text-ok',
  warn: 'text-warn',
  err: 'text-err',
  neutral: 'text-dim',
};

interface StatCardProps {
  /** What is being counted, in the operator's words. Label role, muted. */
  label: string;
  /** The number itself. Display role, tabular. */
  value: string | number;
  /** Trailing qualifier set small and dim next to the value - "/ 3", "GB". */
  unit?: string;
  /** One line of mono detail under the value: which node, what is pending. */
  context?: string;
  /** Movement since the last comparable window, coloured by what it means. */
  delta?: { text: string; tone: DeltaTone };
  /** Small tag at the right of the label - an exception worth naming ("1 offline"). */
  badge?: string;
  loading?: boolean;
}

/**
 * Dashboard stat tile.
 *
 * Four of these sit in a row, and the row's job is to be read in one sweep -
 * so the tiles are identical: no accent border, no icon, no fill. The only
 * thing that varies is the number, which is why the number is the only thing
 * set large. Colour appears solely in `delta`, where it carries meaning
 * (green = good movement, red = bad), never as decoration.
 */
export function StatCard({ label, value, unit, context, delta, badge, loading }: StatCardProps) {
  return (
    <div className="rounded-surface border border-hairline bg-card px-4 py-3.5">
      <div className="flex items-center justify-between gap-2">
        <p className="truncate text-label text-muted-foreground">{label}</p>
        {badge && !loading && <Badge>{badge}</Badge>}
      </div>

      {loading ? (
        <Skeleton className="mt-2 h-6 w-20" />
      ) : (
        <p className="mt-1.5 text-display tabular text-foreground">
          {value}
          {unit && <span className="pl-1 text-body font-normal text-dim">{unit}</span>}
        </p>
      )}

      {loading ? (
        <Skeleton className="mt-2 h-3 w-28" />
      ) : (
        (context || delta) && (
          <p className="mono mt-0.5 flex items-baseline gap-1.5 text-micro">
            {delta && <span className={cn('shrink-0', DELTA_CLASS[delta.tone])}>{delta.text}</span>}
            {context && <span className="truncate text-mute">{context}</span>}
          </p>
        )
      )}
    </div>
  );
}
