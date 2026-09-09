import { useTranslation } from 'react-i18next';

import type { ReactNode } from 'react';

/** A measurement the panel may simply not have. Absence is part of the type, never a zero. */
export type Metric<T = number> = T | null | undefined;

export function isMetricPresent<T>(value: Metric<T>): value is T {
  if (value === null || value === undefined) return false;
  return !(typeof value === 'number' && !Number.isFinite(value));
}

/** The same absent/present split for places that need a string: aria labels, tooltips, chart legends. */
export function useMetricText() {
  const { t } = useTranslation();

  return function metricText<T>(value: Metric<T>, format?: (value: T) => ReactNode): string {
    if (!isMetricPresent(value)) return t('common.not_available');
    return String(format ? format(value) : value);
  };
}
