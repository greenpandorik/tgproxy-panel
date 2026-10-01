import { cn } from '@/lib/utils';

import type { ReactNode } from 'react';

/** One part of an open section: a small heading with its note and controls, then the content. */
export function SubSection({
  title,
  meta,
  actions,
  className,
  children,
}: {
  title: string;
  /** A short note beside the heading: a time, a count. */
  meta?: ReactNode;
  actions?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section className={cn('flex flex-col gap-3', className)}>
      <div className="flex min-h-8 flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <h3 className="text-body font-semibold text-foreground">{title}</h3>
        {(meta || actions) && (
          <div className="flex flex-wrap items-center gap-2">
            {meta && <span className="text-label text-mute">{meta}</span>}
            {actions}
          </div>
        )}
      </div>
      {children}
    </section>
  );
}

/** The parts of an open section, one under another with a hairline between them. */
export function SubSections({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div
      className={cn('flex flex-col divide-y divide-hairline [&>*]:py-5 [&>*:first-child]:pt-2 [&>*:last-child]:pb-0', className)}
    >
      {children}
    </div>
  );
}
