import { Ban, CircleHelp } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { EmptyState } from '@/components/common/EmptyState';
import { formatDateTime } from '@/lib/format';

import type { CapabilityState } from './capability';
import type { ReactNode } from 'react';

interface CapabilityNoticeProps {
  /** Only 'unsupported' and 'undetermined' draw a notice; 'supported' renders the feature. */
  state: Exclude<CapabilityState, 'supported'>;
  /** The feature in the operator's words, e.g. "carrier learning". */
  feature: string;
  /** When the panel last worked the capability set out, for the undetermined wording. */
  checkedAt?: string | null;
  action?: ReactNode;
  className?: string;
}

/**
 * Why a WEB feature is not on screen. "The node does not have it" and "the panel never found
 * out" are different answers and are drawn differently - neither is an error, and neither is
 * an empty measurement.
 */
export function CapabilityNotice({ state, feature, checkedAt, action, className }: CapabilityNoticeProps) {
  const { t, i18n } = useTranslation();

  if (state === 'unsupported') {
    return (
      <div data-capability-state="unsupported" className={className}>
        <EmptyState
          icon={Ban}
          tone="neutral"
          role="status"
          title={t('web.capability_unsupported_title', { feature })}
          description={t('web.capability_unsupported_description')}
          action={action}
          className="border-0 py-10"
        />
      </div>
    );
  }

  return (
    <div data-capability-state="undetermined" className={className}>
      <EmptyState
        icon={CircleHelp}
        tone="info"
        role="status"
        title={t('web.capability_undetermined_title', { feature })}
        description={
          checkedAt
            ? t('web.capability_undetermined_description_checked', { at: formatDateTime(checkedAt, i18n.language) })
            : t('web.capability_undetermined_description')
        }
        action={action}
        className="border-0 py-10"
      />
    </div>
  );
}
