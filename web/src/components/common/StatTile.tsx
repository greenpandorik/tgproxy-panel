import { Link } from 'react-router-dom';

import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

import type { StatTone } from './statTone';
import type { LucideIcon } from 'lucide-react';
import type { CSSProperties } from 'react';

/**
 * The tone tokens, as the custom property `.tgwp-tone-tint` reads. Neutral is
 * --mute rather than a hue: a fact with no state is chrome-coloured, and the
 * tile only lights up when the fact behind it means something.
 */
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

/**
 * One fact from the dashboard, as a tile.
 *
 * The design comes straight from the approved mockup: a 16px glyph on a 10%
 * tint of its own tone with a 20% border, the caption in the label role and
 * the number in display. The colour lives entirely in that plate - caption,
 * number and context stay --mute / --fg / --mute, because a page of eleven
 * tiles whose text was tinted would be a page with no text hierarchy at all.
 *
 * Which tone the plate takes is not this component's decision. It is
 * `statTone`, so the rule that a tile is neutral until its fact means a
 * problem is written once and tested, rather than eleven times by eye.
 *
 * A tile that leads somewhere becomes a link and gets the panel's standard
 * press plus a hover that moves colour only: the ground steps to --bg-3, so
 * nothing under it reflows.
 */
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
          className="tgwp-tone-tint flex size-7 shrink-0 items-center justify-center rounded-control border"
          style={{ '--tone': TONE_VAR[tone] } as CSSProperties}
          aria-hidden="true"
        >
          <Icon size={16} strokeWidth={1.8} />
        </span>
        <span className="truncate text-label text-mute">{label}</span>
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
        <p className="mono mt-1 truncate text-mono text-mute">{[delta, context].filter(Boolean).join(' ') || '\u00a0'}</p>
      )}
    </>
  );

  const shell = cn(
    'block rounded-surface border border-hairline-strong bg-card px-3.5 py-3',
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
