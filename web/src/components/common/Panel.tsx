import { cn } from '@/lib/utils';

import type { LucideIcon } from 'lucide-react';
import type { CSSProperties, ReactNode } from 'react';

/**
 * The panel is the page's only container: a box on --bg-2 with a header rule.
 * The outline is the *stronger* hairline (--line-2), not the separating one:
 * a card that shares its border weight with the rules inside it does not read
 * as a card, it reads as a region of the page that happens to have lines in
 * it. --line stays what it is for: the header rule, the row dividers, the
 * hairlines a panel draws inside itself.
 *
 * Its header carries a mono note on the right, and that note is the point of
 * the device - it says what the panel is showing right now (the window of a
 * chart, how many rows are in a table, how many alerts are open), so the
 * operator never has to guess the scope of what they are reading.
 */
export function Panel({ className, style, children }: { className?: string; style?: CSSProperties; children: ReactNode }) {
  return (
    <section className={cn('overflow-hidden rounded-surface border border-hairline-strong bg-card', className)} style={style}>
      {children}
    </section>
  );
}

interface PanelHeaderProps {
  /**
   * The panel's own glyph, at the left of the title. It is not decoration:
   * it is the same icon the sidebar gives this subject, so a panel and the
   * nav item it belongs to are recognisable as the same thing.
   */
  icon?: LucideIcon;
  title: string;
  /** Right-hand mono note: a range, a count, a step. */
  meta?: ReactNode;
  /** Controls sitting on the right instead of (or after) the note. */
  actions?: ReactNode;
  className?: string;
}

export function PanelHeader({ icon: Icon, title, meta, actions, className }: PanelHeaderProps) {
  return (
    <div
      className={cn(
        // min-h + padding rather than a fixed height, so a header carrying
        // controls wraps onto a second line at 390px instead of truncating its
        // title to nothing and pushing the button past the edge. With nothing
        // to wrap it still measures the standard 44px, which is what the
        // title role's 24px line plus 2x8px of padding needs.
        'flex min-h-14 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b border-hairline px-5 py-3',
        className,
      )}
    >
      <div className="flex min-w-0 items-center gap-2.5">
        {Icon && <Icon size={16} strokeWidth={1.8} className="shrink-0 text-mute" aria-hidden="true" />}
        <h2 className="truncate text-title text-foreground">{title}</h2>
      </div>
      <div className="flex shrink-0 flex-wrap items-center gap-2">
        {meta !== undefined && meta !== null && meta !== '' && <span className="text-label text-mute">{meta}</span>}
        {actions}
      </div>
    </div>
  );
}

export function PanelBody({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn('p-5', className)}>{children}</div>;
}
