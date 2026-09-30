import { useTranslation } from 'react-i18next';

import { cn } from '@/lib/utils';

import type { KeyState } from '@/api/types';

const STATE_DOT: Record<KeyState, string> = {
  active: 'bg-online',
  pending: 'bg-pending',
  disabled: 'bg-dim',
  expired: 'bg-warn',
  revoked: 'bg-offline',
};

export function StateBadge({ state, className }: { state: KeyState; className?: string }) {
  const { t } = useTranslation();
  return (
    <span className={cn('inline-flex items-center gap-2 text-body whitespace-nowrap', className)}>
      <span className={cn('inline-flex size-[7px] shrink-0 rounded-pill', STATE_DOT[state])} aria-hidden="true" />
      <span className="text-foreground">{t(`users.state_${state}`)}</span>
    </span>
  );
}
