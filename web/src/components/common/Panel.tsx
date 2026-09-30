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
        'flex min-h-16 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b border-hairline px-(--panel-x) py-3',
        className,
      )}
    >
      <div className="flex min-w-0 grow basis-32 items-center gap-3">
        {Icon && (
          <span
            className="flex size-9 shrink-0 items-center justify-center rounded-control bg-primary/12 text-brand-primary"
            aria-hidden="true"
          >
            <Icon size={18} strokeWidth={1.8} />
          </span>
        )}
        <h2 className="truncate text-title text-foreground" title={title}>
          {title}
        </h2>
      </div>
      <div className="flex max-w-full shrink-0 flex-wrap items-center gap-2">
        {meta !== undefined && meta !== null && meta !== '' && <span className="text-label text-mute">{meta}</span>}
        {actions}
      </div>
    </div>
  );
}

export function PanelBody({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn('px-(--panel-x) py-5', className)}>{children}</div>;
}
