import type { ReactNode } from 'react';

import { cn } from '@/lib/utils';

/**
 * The panel is the page's only container: a hairline box on --bg-2 with a
 * header rule. Its header carries a mono note on the right, and that note is
 * the point of the device - it says what the panel is showing right now (the
 * window of a chart, how many rows are in a table, how many alerts are open),
 * so the operator never has to guess the scope of what they are reading.
 */
export function Panel({ className, children }: { className?: string; children: ReactNode }) {
  return <section className={cn('overflow-hidden rounded-lg border border-hairline bg-card', className)}>{children}</section>;
}

interface PanelHeaderProps {
  title: string;
  /** Right-hand mono note: a range, a count, a step. */
  meta?: ReactNode;
  /** Controls sitting on the right instead of (or after) the note. */
  actions?: ReactNode;
  className?: string;
}

export function PanelHeader({ title, meta, actions, className }: PanelHeaderProps) {
  return (
    <div
      className={cn(
        // min-h + padding rather than a fixed height, so a header carrying
        // controls wraps onto a second line at 390px instead of truncating its
        // title to nothing and pushing the button past the edge. With nothing
        // to wrap it still measures the standard 40px.
        'flex min-h-10 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b border-hairline px-4 py-1.5',
        className,
      )}
    >
      <h2 className="truncate text-sm font-medium text-foreground">{title}</h2>
      <div className="flex shrink-0 flex-wrap items-center gap-2">
        {meta !== undefined && meta !== null && meta !== '' && <span className="mono text-xs text-dim">{meta}</span>}
        {actions}
      </div>
    </div>
  );
}

export function PanelBody({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn('p-4', className)}>{children}</div>;
}
