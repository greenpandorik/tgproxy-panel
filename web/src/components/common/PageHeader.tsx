import type { ReactNode } from 'react';

interface PageHeaderProps {
  title: string;
  /** Description below the title: scope, guidance or freshness. */
  description?: ReactNode;
  actions?: ReactNode;
}

export function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <div className="flex flex-col items-stretch justify-between gap-3 sm:flex-row sm:items-center">
      <div className="flex min-w-0 flex-col gap-1.5 sm:flex-1">
        <h1 className="text-display text-foreground">{title}</h1>
        {description && <p className="max-w-[72ch] text-body text-mute">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2 sm:shrink-0">{actions}</div>}
    </div>
  );
}
