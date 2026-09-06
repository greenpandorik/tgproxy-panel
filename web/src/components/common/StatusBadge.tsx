import { useTranslation } from 'react-i18next';

import { cn } from '@/lib/utils';

export type Status = 'online' | 'offline' | 'degraded' | 'pending' | 'active' | 'revoked';

const DOT_CLASS: Record<Status, string> = {
  online: 'bg-online',
  active: 'bg-online',
  offline: 'bg-offline',
  revoked: 'bg-offline',
  degraded: 'bg-degraded',
  pending: 'bg-pending',
};

// "Live" states get the signature pulse ring; terminal states (offline/revoked) stay still.
const PULSING: Partial<Record<Status, boolean>> = { online: true, active: true, degraded: true };

interface StatusBadgeProps {
  status: Status;
  label?: string;
  className?: string;
  /** Renders only the dot (still announced to screen readers via visually-hidden text) - for dense table rows. */
  hideLabel?: boolean;
}

/**
 * Status is a 7px dot plus a word, never a filled pill: in a table of thirty
 * rows the dots form a column you can scan, while thirty coloured pills would
 * be the loudest thing on the page. Live states (online, active, degraded)
 * carry a soft pulsing ring - the panel's one recurring motif - which
 * prefers-reduced-motion turns off (see .tgwp-pulse-ring in index.css).
 */
export function StatusBadge({ status, label, className, hideLabel }: StatusBadgeProps) {
  const { t } = useTranslation();
  const text = label ?? t(`common.${status}`);

  return (
    <span className={cn('inline-flex items-center gap-2 text-sm whitespace-nowrap', className)}>
      <span className="relative inline-flex size-[7px] shrink-0">
        {PULSING[status] && (
          <span className={cn('tgwp-pulse-ring absolute inset-0 rounded-full', DOT_CLASS[status])} aria-hidden="true" />
        )}
        <span className={cn('relative inline-flex size-[7px] rounded-full', DOT_CLASS[status])} />
      </span>
      <span className={cn('text-foreground', hideLabel && 'sr-only')}>{text}</span>
    </span>
  );
}
