import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Check, Loader2, CircleAlert } from 'lucide-react';
import { api } from '@/lib/api';
import { nodeKeys } from '@/api/nodes';
import type { Node } from '@/api/types';
import { useAuth } from '@/auth/AuthProvider';
import { AdvancedSettings } from '@/components/common/AdvancedSettings';
import { ErrorState } from '@/components/common/ErrorState';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { formatDateTime } from '@/lib/format';

interface UpdateJob {
  id: string;
  status: string;
  outcome: string;
  from_version: string;
  to_version: string;
  error: string;
  started_at: string;
  finished_at: string | null;
  steps: { key: string; state: string; message: string; remaining_sessions: number; remaining_streams: number }[];
}
interface UpdateHistory {
  items: UpdateJob[];
  running: UpdateJob | null;
  installed_version: string;
  pinned_version: string;
  update_available: string;
}
/** The telemt build on a server against the one the panel recommends, and the update that brings it there. */
export function TelemtUpdateCard({ node }: { node: Node }) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const qc = useQueryClient();
  const [confirm, setConfirm] = useState(false);
  const query = useQuery({
    queryKey: ['telemt-update', node.id],
    queryFn: () => api.get<UpdateHistory>(`/api/v1/nodes/${node.id}/telemt-update`),
    refetchInterval: (q) => (q.state.data?.running ? 2000 : 30000),
  });
  const start = useMutation({
    mutationFn: () =>
      api.post<UpdateJob>(`/api/v1/nodes/${node.id}/telemt-update`, { require_drain: true, drain_timeout_secs: 120 }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['telemt-update', node.id] });
      void qc.invalidateQueries({ queryKey: nodeKeys.one(node.id) });
    },
  });
  const latest = query.data?.items[0];
  const running = !!query.data?.running;
  const normalizedInstalled = query.data?.installed_version?.replace(/^v/, '');
  const normalizedPinned = query.data?.pinned_version?.replace(/^v/, '');
  const upToDate = !!normalizedInstalled && normalizedInstalled === normalizedPinned;
  return (
    <div className="space-y-4">
      {query.isLoading ? (
        <Skeleton className="h-28" />
      ) : query.isError ? (
        <ErrorState inset message={query.error.message} onRetry={() => void query.refetch()} retryLabel={t('common.refresh')} />
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div className="flex flex-wrap gap-6">
              <div>
                <p className="text-label text-mute">{t('web.update_installed')}</p>
                <p className="mono text-mono text-foreground">telemt {query.data?.installed_version || '—'}</p>
              </div>
              <div>
                <p className="text-label text-mute">{t('web.update_recommended')}</p>
                <p className="mono text-mono text-foreground">{query.data?.pinned_version || '—'}</p>
              </div>
            </div>
            {upToDate ? (
              <span className="inline-flex items-center gap-1.5 text-label text-ok">
                <Check size={16} />
                {t('web.update_up_to_date')}
              </span>
            ) : (
              isWriter && (
                <Button size="sm" disabled={!node.online || running || start.isPending} onClick={() => setConfirm(true)}>
                  {t(running ? 'web.update_running' : 'web.update_action')}
                </Button>
              )
            )}
          </div>
          {isWriter && !upToDate && !node.online && <p className="text-label text-mute">{t('web.update_offline')}</p>}
          {latest && (
            <div className="space-y-3 border-t border-hairline pt-4" role="status" aria-live="polite">
              <p className="font-medium">
                {t(`web.update_status_${latest.status}`, latest.status)}{' '}
                <span className="ml-2 text-label font-normal text-mute">{formatDateTime(latest.started_at, i18n.language)}</span>
              </p>
              <ol className="space-y-2">
                {latest.steps.map((step, i) => (
                  <li key={`${step.key}-${i}`} className="flex items-start gap-2 text-label">
                    {step.state === 'running' ? (
                      <Loader2 size={16} className="mt-0.5 animate-spin motion-reduce:animate-none" />
                    ) : step.state === 'ok' ? (
                      <Check size={16} className="mt-0.5 text-ok" />
                    ) : (
                      <CircleAlert size={16} className="mt-0.5 text-mute" />
                    )}
                    <div>
                      <span>{t(`web.update_step_${step.key}`, step.key)}</span>
                      {step.key === 'drain' && step.state === 'running' && (
                        <span className="ml-2 text-mute">
                          {t('web.update_remaining', { sessions: step.remaining_sessions, streams: step.remaining_streams })}
                        </span>
                      )}
                      <details className="text-mute">
                        <summary className="cursor-pointer">{t('web.update_details')}</summary>
                        <p className="mt-1 break-words">{step.message}</p>
                      </details>
                    </div>
                  </li>
                ))}
              </ol>
              {latest.error && (
                <p className="text-body text-destructive" role="alert">
                  {latest.error}
                </p>
              )}
            </div>
          )}
          {(query.data?.items.length ?? 0) > 1 && (
            <AdvancedSettings label={t('web.update_history')}>
              <ul className="space-y-2">
                {query.data?.items.slice(1).map((job) => (
                  <li key={job.id} className="text-label">
                    {formatDateTime(job.started_at, i18n.language)} · {job.from_version} → {job.to_version} ·{' '}
                    {t(`web.update_status_${job.status}`, job.status)}
                  </li>
                ))}
              </ul>
            </AdvancedSettings>
          )}
        </>
      )}
      {start.isError && (
        <p role="alert" className="text-body text-destructive">
          {start.error.message}
        </p>
      )}
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={t('web.update_confirm_title')}
        description={t('web.update_confirm_hint')}
        confirmLabel={t('web.update_action')}
        onConfirm={() => start.mutateAsync().then(() => undefined)}
      />
    </div>
  );
}
