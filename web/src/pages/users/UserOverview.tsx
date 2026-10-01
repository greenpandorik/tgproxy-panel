import { ExternalLink, Link2, Plus, QrCode, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { CopyButton } from '@/components/common/CopyButton';
import { RowChevron } from '@/components/common/RowChevron';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { HelpButton } from '@/help';
import { formatBytes, formatDate, formatDateTime, formatRelativeTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import { TONE_TEXT, expiryTone } from './userState';

import type { AccessKey } from '@/api/types';
import type { ReactNode } from 'react';

/** Online with about how many devices, or when the person was last seen. */
export function UserPresence({ user }: { user: AccessKey }) {
  const { t, i18n } = useTranslation();
  if (user.live.online) {
    return (
      <span className="inline-flex items-center gap-2 text-ok" title={t('users.online_now', { count: user.live.connections })}>
        <span className="size-[7px] shrink-0 rounded-pill bg-online" aria-hidden="true" />
        {t('users.online_devices', { count: user.live.devices })}
      </span>
    );
  }
  if (!user.last_seen_at) return <span>{t('users.never_seen')}</span>;
  return (
    <span title={formatDateTime(user.last_seen_at, i18n.language)}>
      {t('users.presence_offline', { value: formatRelativeTime(user.last_seen_at, i18n.language) })}
    </span>
  );
}

export function AccessSwitch({
  user,
  disabled,
  onToggle,
}: {
  user: AccessKey;
  disabled: boolean;
  onToggle: (on: boolean) => void;
}) {
  const { t } = useTranslation();
  const on = user.state !== 'disabled';
  return (
    <label className="flex cursor-pointer items-center justify-between gap-4 rounded-surface border border-hairline px-4 py-3">
      <span className="min-w-0">
        <span className="block text-body font-semibold">{on ? t('users.access_on') : t('users.access_off')}</span>
        <span className="block text-label text-mute">{t('users.access_toggle_hint')}</span>
      </span>
      <Switch checked={on} disabled={disabled} onCheckedChange={(next) => onToggle(next)} />
    </label>
  );
}

function Fact({ label, value, sub, tone }: { label: string; value: ReactNode; sub?: string; tone?: string }) {
  return (
    <div className="min-w-0 rounded-surface border border-hairline bg-surface px-3 py-2.5">
      <p className="truncate text-label text-mute">{label}</p>
      <p className={cn('mt-0.5 truncate text-body font-semibold', tone)}>{value}</p>
      {sub && (
        <p className="truncate text-label text-mute" title={sub}>
          {sub}
        </p>
      )}
    </div>
  );
}

/** Expiry, traffic, servers and last seen, as four short facts. */
export function UserFacts({ user, measured, nodesTotal }: { user: AccessKey; measured: boolean; nodesTotal: number }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const expires = user.expires_at;
  const seen = user.live.online
    ? {
        value: t('users.fact_seen_now'),
        sub: `${t('users.online_now', { count: user.live.connections })} · ${t('users.devices_15m', { count: user.live.devices_15m })}`,
      }
    : user.last_seen_at
      ? { value: formatRelativeTime(user.last_seen_at, lang), sub: formatDateTime(user.last_seen_at, lang) }
      : { value: t('users.fact_seen_never'), sub: undefined };

  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
      <Fact
        label={t('users.fact_expires')}
        value={expires ? t('users.fact_expires_until', { date: formatDate(expires, lang) }) : t('keys.no_expiry')}
        sub={expires ? formatRelativeTime(expires, lang) : undefined}
        tone={expires ? TONE_TEXT[expiryTone(expires)] : undefined}
      />
      <Fact
        label={t('users.column_traffic')}
        value={measured ? formatBytes(user.traffic_30d) : '—'}
        sub={measured ? undefined : t('users.traffic_unmeasured')}
      />
      <Fact
        label={t('users.column_servers')}
        value={t('users.fact_servers_value', { count: user.nodes.length, total: Math.max(nodesTotal, user.nodes.length) })}
        sub={user.nodes.map((n) => n.node_name || n.hostname).join(', ') || undefined}
      />
      <Fact label={t('users.fact_seen')} value={seen.value} sub={seen.sub} tone={user.live.online ? 'text-ok' : undefined} />
    </div>
  );
}

interface SubscriptionBlockProps {
  user: AccessKey;
  canIssue: boolean;
  onShowQr: () => void;
  onIssueLink: () => void;
}

/** The link the person gets: copy it, show it as a QR code, open the page it leads to. */
export function SubscriptionBlock({ user, canIssue, onShowQr, onIssueLink }: SubscriptionBlockProps) {
  const { t } = useTranslation();
  const link = user.subscription_short_url ?? user.subscription_url;
  return (
    <section className="space-y-2" aria-labelledby="user-subscription-title">
      <div className="flex items-center gap-2">
        <h3 id="user-subscription-title" className="text-body font-semibold">
          {t('keys.subscription_title')}
        </h3>
        <HelpButton topic="keys.link" className="-my-1" />
      </div>
      {link ? (
        <>
          <div className="flex flex-col gap-2 sm:flex-row">
            <Input
              readOnly
              value={link}
              className="mono min-w-0 flex-1 text-mono"
              onFocus={(e) => e.target.select()}
              aria-label={t('keys.subscription_title')}
            />
            <div className="flex gap-2">
              <CopyButton value={link} label={t('common.copy')} showLabel className="flex-1 sm:flex-none" />
              <Button type="button" variant="outline" className="flex-1 sm:flex-none" onClick={onShowQr}>
                <QrCode />
                {t('users.show_qr')}
              </Button>
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
            {user.subscription_short_url && user.subscription_url ? (
              <p className="mono min-w-0 flex-1 truncate text-mono text-mute" title={user.subscription_url}>
                {t('users.full_link')}: {user.subscription_url}
              </p>
            ) : (
              <span />
            )}
            <Button
              type="button"
              variant="link"
              size="sm"
              className="h-auto px-0"
              nativeButton={false}
              render={<a href={link} target="_blank" rel="noopener noreferrer" />}
            >
              {t('users.open_page')}
              <ExternalLink />
            </Button>
          </div>
        </>
      ) : (
        <div className="space-y-2 rounded-surface border border-dashed border-hairline-strong p-3">
          <p className="text-label text-mute">
            {user.subscription_legacy ? t('users.subscription_legacy') : t('users.subscription_none')}
          </p>
          {canIssue && (
            <Button type="button" size="sm" onClick={onIssueLink}>
              {user.subscription_legacy ? <RefreshCw /> : <Plus />}
              {user.subscription_legacy ? t('users.subscription_reissue') : t('keys.subscription_create')}
            </Button>
          )}
        </div>
      )}
    </section>
  );
}

/** One row that opens the per-server links, with how many there are when the panel knows. */
export function DirectLinksRow({ count, onOpen }: { count: number | undefined; onOpen: () => void }) {
  const { t } = useTranslation();
  return (
    <button
      type="button"
      onClick={onOpen}
      className="group/row flex w-full items-center gap-3 rounded-surface border border-hairline px-4 py-3 text-left transition-[background-color,border-color,scale] outline-none hover:border-hairline-strong hover:bg-elevated focus-visible:ring-2 focus-visible:ring-ring active:scale-[0.985]"
    >
      <Link2 className="size-4 shrink-0 text-mute" aria-hidden="true" />
      <span className="min-w-0 flex-1 text-body font-semibold">{t('users.direct_links')}</span>
      {count !== undefined && <span className="shrink-0 text-label text-mute">{t('users.direct_links_count', { count })}</span>}
      <RowChevron />
    </button>
  );
}
