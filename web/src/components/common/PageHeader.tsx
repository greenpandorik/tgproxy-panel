import type { ReactNode } from 'react';

interface PageHeaderProps {
  title: string;
  /** Mono note beside the title - a freshness stamp, a scope. */
  description?: ReactNode;
  actions?: ReactNode;
}

/**
 * Page heading: the display role, with the page's own actions on the right and
 * an optional mono note beside the title. No rule under it - the first panel
 * below already draws one, and two lines 12px apart is a stack, not a
 * hierarchy.
 *
 * This is the only display-sized text on a page other than a KPI number, and
 * a page has exactly one of these.
 */
export function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
        <h1 className="text-display text-foreground">{title}</h1>
        {description && <p className="mono truncate text-mono text-dim">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}
