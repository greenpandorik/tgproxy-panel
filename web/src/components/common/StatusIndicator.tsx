import { ArrowDownToLine, CircleCheck, CircleHelp, CircleSlash, CloudDownload, RefreshCw, TriangleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { cn } from '@/lib/utils';

import { HEALTH_TONE } from './healthStatus';
import { TONE_VAR } from './statTone';

import type { HealthStatus } from './healthStatus';
import type { LucideIcon } from 'lucide-react';

export type { HealthStatus };

const ICON: Record<HealthStatus, LucideIcon> = {
  healthy: CircleCheck,
  degraded: TriangleAlert,
  offline: CircleSlash,
  installing: CloudDownload,
  updating: RefreshCw,
  draining: ArrowDownToLine,
  unknown: CircleHelp,
};

const PULSING: Partial<Record<HealthStatus, boolean>> = {
  healthy: true,
  degraded: true,
  installing: true,
  updating: true,
  draining: true,
};

interface StatusIndicatorProps {
  status: HealthStatus;
  /** Overrides the vocabulary wording; the icon and tone stay tied to the status. */
  label?: string;
  className?: string;
  /** Icon only, for dense table rows. The wording stays in the accessibility tree. */
  hideLabel?: boolean;
}

/** Colour, icon and text for one status - never colour alone. */
export function StatusIndicator({ status, label, className, hideLabel }: StatusIndicatorProps) {
  const { t } = useTranslation();
  const tone = HEALTH_TONE[status];
  const text = label ?? t(`common.status.${status}`);
  const Icon = ICON[status];

  return (
    <span
      data-status={status}
      data-tone={tone}
      className={cn('inline-flex items-center gap-2 text-body whitespace-nowrap', className)}
    >
      <span className="relative inline-flex size-4 shrink-0 items-center justify-center" style={{ color: TONE_VAR[tone] }}>
        {PULSING[status] && (
          <span
            className="tgwp-pulse-ring absolute inset-[5px] rounded-pill"
            style={{ backgroundColor: TONE_VAR[tone] }}
            aria-hidden="true"
          />
        )}
        <Icon size={14} strokeWidth={2} className="relative" aria-hidden="true" />
      </span>
      <span className={cn('text-foreground', hideLabel && 'sr-only')}>{text}</span>
    </span>
  );
}
