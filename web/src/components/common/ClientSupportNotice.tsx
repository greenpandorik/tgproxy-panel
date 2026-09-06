import { useTranslation } from 'react-i18next';

import { cn } from '@/lib/utils';

import type { ClientSupport } from '@/api/types';

const PLATFORMS: (keyof ClientSupport)[] = ['desktop', 'android', 'ios'];

const DEFAULT_SUPPORT: ClientSupport = { desktop: 'stable', android: 'experimental', ios: 'planned' };

/** How settled each platform's support is, on the panel's own three-step status scale. */
const DOT_CLASS: Record<string, string> = {
  stable: 'bg-ok',
  experimental: 'bg-warn',
  planned: 'bg-pending',
};

interface ClientSupportNoticeProps {
  clientSupport?: ClientSupport;
  className?: string;
}

/**
 * Which Telegram clients can actually open the link that was just handed out.
 *
 * Shown wherever a t.me/tg:// link appears. It is a fact about the world, not
 * a warning, so it gets no coloured panel - just a hairline row and the same
 * dot the rest of the panel uses for status, one per platform, so "Android is
 * amber" is legible without reading the word next to it.
 */
export function ClientSupportNotice({ clientSupport, className }: ClientSupportNoticeProps) {
  const { t } = useTranslation();
  const support = clientSupport ?? DEFAULT_SUPPORT;

  return (
    <div className={cn('flex flex-wrap items-center gap-x-4 gap-y-2 rounded-md border border-hairline px-3 py-2', className)}>
      <p className="text-xs text-mute">{t('keys.client_support_title')}</p>
      <ul className="flex flex-wrap gap-x-4 gap-y-1.5">
        {PLATFORMS.map((platform) => (
          <li key={platform} className="mono flex items-center gap-1.5 text-xs">
            <span
              className={cn('size-[7px] shrink-0 rounded-full', DOT_CLASS[support[platform]] ?? 'bg-pending')}
              aria-hidden="true"
            />
            <span className="text-foreground">{t(`keys.platform_${platform}`)}</span>
            <span className="text-dim">{t(`keys.support_status_${support[platform]}`, support[platform])}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
