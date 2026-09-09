import { useTranslation } from 'react-i18next';

import { cn } from '@/lib/utils';

import { isMetricPresent } from './metric';

import type { Metric } from './metric';
import type { ReactNode } from 'react';

interface MetricValueProps<T> {
  /** The raw measurement. Pass it through unchanged - a `?? 0` here is the bug this component exists to prevent. */
  value: Metric<T>;
  /** Runs only once the value is known to exist, so formatting code never sees an absent metric. */
  format?: (value: T) => ReactNode;
  /** Trailing qualifier, dropped along with the value when there is nothing to qualify. */
  unit?: string;
  /** Dense rows: an em dash on screen, the full wording for screen readers and on hover. */
  compact?: boolean;
  className?: string;
}

/** One measurement, or an honest "Not available" - a real 0 still renders as 0. */
export function MetricValue<T extends number | string>({ value, format, unit, compact, className }: MetricValueProps<T>) {
  const { t } = useTranslation();

  if (!isMetricPresent(value)) {
    const text = t('common.not_available');
    return (
      <span data-metric="absent" title={compact ? text : undefined} className={cn('text-mute', className)}>
        {compact ? (
          <>
            <span aria-hidden="true">&mdash;</span>
            <span className="sr-only">{text}</span>
          </>
        ) : (
          text
        )}
      </span>
    );
  }

  return (
    <span data-metric="present" className={cn('tabular text-foreground', className)}>
      {format ? format(value) : value}
      {unit && <span className="ml-1 text-mute">{unit}</span>}
    </span>
  );
}
