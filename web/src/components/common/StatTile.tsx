import { Link } from 'react-router-dom';

import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

import type { StatTone } from './statTone';
import type { LucideIcon } from 'lucide-react';
import type { CSSProperties } from 'react';

// The tone tokens, as the custom property `.tgwp-tone-tint` reads.
const TONE_VAR: Record<StatTone, string> = {
  neutral: 'var(--mute)',
  ok: 'var(--status-ok)',
  warn: 'var(--status-warn)',
  err: 'var(--status-err)',
  info: 'var(--status-info)',
};

export interface StatTileProps {
  /** The glyph on the tinted plate. 16px, from lucide. */
  icon: LucideIcon;
  /** Which tone the plate takes. Decide it with `statTone`, never by hand. */
  tone?: StatTone;
  /** What is being counted, in the operator's words. Label role, muted. */
  label: string;
  /** The number itself. Display role, tabular, always --fg. */
  value: string | number;
  /** Trailing qualifier set small next to the value: "/ 3", "GB", "%". */
  unit?: string;
  /** One mono line under the value: which node, what is queued, over what window. */
  context?: string;
  /** Movement since the last comparable window, e.g. "▼ 29% in the last hour". */
  delta?: string;
  /** Where the tile leads, if it leads anywhere. Adds the press and a hover. */
  to?: string;
  loading?: boolean;
  className?: string;
  style?: CSSProperties;
}

// One fact from the dashboard, as a tile.
export function StatTile({
  icon: Icon,
  tone = 'neutral',
  label,
  value,
  unit,
  context,
  delta,
  to,
  loading,
  className,
  style,
}: StatTileProps) {
  const body = (
    <>
      <div className="flex items-center gap-2.5">
        <span
          className="tgwp-tone-tint flex size-7 shrink-0 sm:size-9 items-center justify-center rounded-control border"
          style={{ '--tone': TONE_VAR[tone] } as CSSProperties}
          aria-hidden="true"
        >
          <Icon size={16} strokeWidth={1.8} />
        </span>
        <span className="min-w-0 text-label text-mute">{label}</span>
      </div>

      {loading ? (
        <Skeleton className="mt-2.5 h-6 w-16" />
      ) : (
        <p className="mt-2 flex items-baseline gap-1 text-display tabular text-foreground">
          <span className="truncate">{value}</span>
          {unit && <span className="mono shrink-0 text-mono font-normal text-mute">{unit}</span>}
        </p>
      )}

      {/* A tile with nothing to say still keeps this line, so eleven tiles in
          a grid are eleven boxes of one height and the row does not comb. */}
      {loading ? (
        <Skeleton className="mt-2 h-3 w-24" />
      ) : (
        <p className="mt-2 text-label text-mute">{[delta, context].filter(Boolean).join(' ') || '\u00a0'}</p>
      )}
    </>
  );

  const shell = cn(
    'block rounded-surface border border-hairline-strong bg-card px-5 py-4',
    to && 'transition-[background-color,border-color,scale] active:scale-[0.985] hover:bg-elevated',
    className,
  );

  if (to && !loading) {
    return (
      <Link to={to} className={shell} style={style}>
        {body}
      </Link>
    );
  }

  return (
    <div className={shell} style={style}>
      {body}
    </div>
  );
}
