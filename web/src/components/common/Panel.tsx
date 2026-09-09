import { cn } from '@/lib/utils';

import type { LucideIcon } from 'lucide-react';
import type { CSSProperties, ReactNode } from 'react';

// The panel is the page's only container: a box on --bg-2 with a header rule.
export function Panel({ className, style, children }: { className?: string; style?: CSSProperties; children: ReactNode }) {
  return (
    <section className={cn('overflow-hidden rounded-surface border border-hairline-strong bg-card', className)} style={style}>
      {children}
    </section>
  );
}

interface PanelHeaderProps {
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
