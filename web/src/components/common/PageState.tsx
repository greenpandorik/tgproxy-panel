import { CircleSlash, TriangleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

import { EmptyState } from './EmptyState';

import type { ReactNode } from 'react';

const ROW_WIDTHS = ['w-2/5', 'w-full', 'w-4/5', 'w-3/5', 'w-full'];

interface LoadingStateProps {
  /** What is loading, for screen readers. Defaults to the generic wording. */
  label?: string;
  rows?: number;
  /** Inside a panel that already draws the border. */
  inset?: boolean;
  className?: string;
}

/** A surface waiting for its query: quiet placeholder rows, announced once. */
export function LoadingState({ label, rows = 3, inset = false, className }: LoadingStateProps) {
  const { t } = useTranslation();

  return (
    <div
      role="status"
      aria-busy="true"
      aria-live="polite"
      className={cn('flex flex-col gap-3', inset ? 'px-6 py-8' : 'rounded-surface border border-hairline-strong p-5', className)}
    >
      <span className="sr-only">{label ?? t('common.state.loading')}</span>
      {Array.from({ length: rows }, (_, index) => (
        <Skeleton key={index} className={cn('h-4', ROW_WIDTHS[index % ROW_WIDTHS.length])} />
      ))}
    </div>
  );
}

interface StateProps {
  title?: string;
  description?: string;
  /** Buttons under the text: retry, open logs, whatever fixes it.  */
  action?: ReactNode;
  className?: string;
}

/** The surface answered, but only in part: what is shown is real, what is missing is missing. */
export function DegradedState({ title, description, action, className }: StateProps) {
  const { t } = useTranslation();

  return (
    <EmptyState
      icon={TriangleAlert}
      tone="warn"
      role="status"
      title={title ?? t('common.state.degraded_title')}
      description={description ?? t('common.state.degraded_description')}
      action={action}
      className={className}
    />
  );
}

/** Not empty and not broken: the panel could not obtain this at all, so it says so. */
export function NotAvailableState({ title, description, action, className }: StateProps) {
  const { t } = useTranslation();

  return (
    <EmptyState
      icon={CircleSlash}
      tone="neutral"
      role="status"
      title={title ?? t('common.state.not_available_title')}
      description={description ?? t('common.state.not_available_description')}
      action={action}
      className={className}
    />
  );
}
