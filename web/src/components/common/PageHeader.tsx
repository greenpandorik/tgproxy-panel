import type { ReactNode } from 'react';

interface PageHeaderProps {
  title: string;
  /** Mono note beside the title - a freshness stamp, a scope. */
  description?: ReactNode;
  actions?: ReactNode;
}

export function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
        <h1 className="text-display text-foreground">{title}</h1>
        {description && <p className="mono truncate text-mono text-mute">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}
