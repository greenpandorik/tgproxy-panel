import { Link } from 'react-router-dom';

import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

import type { ReactNode } from 'react';

export interface StatStripItem {
  id: string;
  label: string;
  value: ReactNode;
  /** One mono line under the value. */
  sub?: ReactNode;
  tone?: 'ok' | 'warn' | 'err';
  to?: string;
}

const TONE_TEXT = { ok: 'text-ok', warn: 'text-warn', err: 'text-err' } as const;

/** Key numbers in one row: two per line on a phone, one line from md up. */
export function StatStrip({
  items,
  loading,
  label,
  className,
}: {
  items: StatStripItem[];
  loading?: boolean;
  label?: string;
  className?: string;
}) {
  return (
    <div
      role="group"
      aria-label={label}
      className={cn(
        'grid grid-cols-2 gap-px overflow-hidden rounded-surface border border-hairline-strong bg-hairline md:auto-cols-fr md:grid-flow-col md:grid-cols-none',
        items.length % 2 === 1 && '[&>*:last-child]:col-span-2 md:[&>*:last-child]:col-span-1',
        className,
      )}
    >
      {items.map((item) => {
        const body = (
          <>
            <p className="truncate text-label text-mute" title={item.label}>
              {item.label}
            </p>
            {loading ? (
              <Skeleton className="mt-1.5 h-6 w-16" />
            ) : (
              <p className={cn('mt-1 truncate text-title tabular', item.tone ? TONE_TEXT[item.tone] : 'text-foreground')}>
                {item.value}
              </p>
            )}
            {!loading && item.sub && <p className="mono truncate text-micro text-mute">{item.sub}</p>}
          </>
        );
        const cell = 'min-w-0 bg-card px-4 py-3.5 sm:px-5 sm:py-4';
        return item.to && !loading ? (
          <Link
            key={item.id}
            to={item.to}
            className={cn(cell, 'transition-colors hover:bg-elevated focus-visible:relative focus-visible:z-10')}
          >
            {body}
          </Link>
        ) : (
          <div key={item.id} className={cell}>
            {body}
          </div>
        );
      })}
    </div>
  );
}
