import { CircleCheck } from 'lucide-react';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useAlerts, useReadAlerts } from '@/api/dashboard';
import { useAuth } from '@/auth/AuthProvider';
import { ErrorState } from '@/components/common/ErrorState';
import { LoadingState } from '@/components/common/PageState';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';
import { formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';

import { InboxItem } from './InboxItem';

import type { ReactNode } from 'react';

/** How many problems show before «Показать ещё». */
const VISIBLE = 5;

/** The inbox below its header: waiting, broken, all clear, or the list. The box keeps its size in every case. */
function InboxBody({
  loading,
  failed,
  empty,
  onRetry,
  children,
}: {
  loading: boolean;
  failed: boolean;
  empty: boolean;
  onRetry: () => void;
  children: ReactNode;
}) {
  const { t } = useTranslation();
  if (loading) return <LoadingState inset rows={3} label={t('dashboard.alerts_title')} />;
  if (failed) {
    return <ErrorState inset message={t('dashboard.attention_unavailable')} retryLabel={t('common.refresh')} onRetry={onRetry} />;
  }
  if (empty) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 px-6 py-8 text-center">
        <CircleCheck size={22} strokeWidth={1.8} className="text-ok" aria-hidden="true" />
        <p className="text-body text-foreground">{t('dashboard.inbox_all_clear')}</p>
        <p className="text-label text-mute">{t('dashboard.inbox_all_clear_hint')}</p>
      </div>
    );
  }
  return <div className="flex flex-col">{children}</div>;
}

/** What is wrong right now, each with the action that fixes it. A read problem leaves until a new one comes. */
export function AttentionInbox({ className }: { className?: string }) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const alertsQuery = useAlerts();
  const readAlerts = useReadAlerts();
  const headingRef = useRef<HTMLHeadingElement>(null);
  const [picked, setPicked] = useState<Set<number>>(() => new Set());
  const [expanded, setExpanded] = useState(false);
  const [status, setStatus] = useState('');

  const alerts = alertsQuery.data?.items ?? [];
  const selected = alerts.filter((a) => picked.has(a.id)).map((a) => a.id);
  const shown = expanded ? alerts : alerts.slice(0, VISIBLE);
  const hidden = alerts.length - shown.length;
  const writer = isWriter && alerts.length > 0;

  const read = (ids: number[]) => {
    if (ids.length === 0) return;
    readAlerts.mutate(ids, {
      onSuccess: () => setStatus(t('dashboard.inbox_read_done', { count: ids.length })),
      onError: (err) =>
        toast.add({
          title: t('dashboard.inbox_read_failed'),
          description: err instanceof ApiError ? err.message : t('common.error_generic'),
          type: 'error',
        }),
    });
    setPicked((current) => new Set([...current].filter((id) => !ids.includes(id))));
    headingRef.current?.focus();
  };

  const select = (id: number, on: boolean) =>
    setPicked((current) => {
      const next = new Set(current);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });

  const allSelected = alerts.length > 0 && selected.length === alerts.length;

  return (
    <section
      aria-labelledby="inbox-title"
      className={cn('flex min-h-56 flex-col overflow-hidden rounded-surface border border-hairline-strong bg-card', className)}
    >
      <div className="flex min-h-14 flex-wrap items-center gap-x-3 gap-y-2 border-b border-hairline px-(--panel-x) py-2.5">
        {writer && (
          <Checkbox
            checked={allSelected}
            indeterminate={selected.length > 0 && !allSelected}
            onCheckedChange={(v) => setPicked(v ? new Set(alerts.map((a) => a.id)) : new Set())}
            aria-label={t('dashboard.inbox_select_all')}
          />
        )}
        <h2
          id="inbox-title"
          ref={headingRef}
          tabIndex={-1}
          className="flex grow items-baseline gap-2 text-title text-foreground outline-none"
        >
          {t('dashboard.alerts_title')}
          {alerts.length > 0 && <span className="mono text-mono text-mute">{formatNumber(alerts.length, i18n.language)}</span>}
        </h2>
        {writer && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={readAlerts.isPending}
            onClick={() => read(selected.length > 0 ? selected : alerts.map((a) => a.id))}
          >
            {selected.length > 0 ? t('dashboard.inbox_read_selected', { count: selected.length }) : t('dashboard.inbox_read_all')}
          </Button>
        )}
      </div>
      <p role="status" className="sr-only">
        {status}
      </p>
      <InboxBody
        loading={alertsQuery.isLoading}
        failed={alertsQuery.isError}
        onRetry={() => void alertsQuery.refetch()}
        empty={alerts.length === 0}
      >
        <ul className="divide-y divide-hairline">
          {shown.map((alert) => (
            <InboxItem
              key={alert.id}
              alert={alert}
              writer={isWriter}
              selected={picked.has(alert.id)}
              onSelect={(on) => select(alert.id, on)}
              onRead={() => read([alert.id])}
              reading={readAlerts.isPending}
            />
          ))}
        </ul>
        {(hidden > 0 || expanded) && alerts.length > VISIBLE && (
          <Button type="button" variant="ghost" className="m-2 self-start" onClick={() => setExpanded((v) => !v)}>
            {expanded ? t('dashboard.inbox_show_less') : t('dashboard.inbox_show_more', { count: hidden })}
          </Button>
        )}
      </InboxBody>
    </section>
  );
}
