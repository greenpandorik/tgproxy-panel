import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import type { Node } from '@/api/types';
import { useAuth } from '@/auth/AuthProvider';
import { api, ApiError } from '@/lib/api';
import { AdvancedSettings } from '@/components/common/AdvancedSettings';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { formatDateTime } from '@/lib/format';
interface Rollout {
  id: string;
  status: string;
  node_ids: string[];
  cursor: number;
  error: string;
  created_at: string;
}
export function FleetUpdates({ nodes }: { nodes: Node[] }) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const qc = useQueryClient();
  const [selected, setSelected] = useState<string[]>([]);
  const [confirm, setConfirm] = useState(false);
  const query = useQuery({
    queryKey: ['fleet-updates'],
    queryFn: () => api.get<{ items: Rollout[] }>('/api/v1/fleet/updates'),
    refetchInterval: 10000,
  });
  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: ['fleet-updates'] });
  };
  const start = useMutation({
    mutationFn: () => api.post('/api/v1/fleet/updates', { node_ids: selected }),
    onSuccess: () => {
      setConfirm(false);
      setSelected([]);
      invalidate();
    },
  });
  const stop = useMutation({ mutationFn: (id: string) => api.post(`/api/v1/fleet/updates/${id}/stop`), onSuccess: invalidate });
  const active = query.data?.items.find((v) => v.status === 'running');
  const eligible = nodes.filter((n) => n.engine === 'telemt' && n.online);
  const error = start.error ?? stop.error ?? query.error;
  return (
    <AdvancedSettings label={t('fleet.title')}>
      <div className="space-y-4">
        <p className="max-w-[72ch] text-body text-mute">{t('fleet.hint')}</p>
        {error && (
          <p role="alert" className="text-body text-err">
            {error instanceof ApiError ? error.message : t('common.error_generic')}
          </p>
        )}
        {isWriter && !active && (
          <fieldset disabled={start.isPending || query.isLoading || query.isError} className="space-y-3">
            <legend className="mb-3 text-body font-medium">{t('fleet.select')}</legend>
            {eligible.length === 0 ? (
              <p className="text-label text-mute">{t('fleet.empty')}</p>
            ) : (
              eligible.map((n) => (
                <label key={n.id} className="flex min-h-10 items-center gap-3 text-body">
                  <input
                    type="checkbox"
                    className="size-4 accent-primary"
                    checked={selected.includes(n.id)}
                    onChange={(e) => setSelected((v) => (e.target.checked ? [...v, n.id] : v.filter((id) => id !== n.id)))}
                  />
                  {n.name}
                  <span className="text-label text-mute">
                    {n.hostname}
                    {selected.includes(n.id) ? ` · ${selected.indexOf(n.id) + 1}` : ''}
                  </span>
                </label>
              ))
            )}
            <Button disabled={selected.length === 0} onClick={() => setConfirm(true)}>
              {t('fleet.start')}
            </Button>
          </fieldset>
        )}
        {(query.data?.items ?? []).slice(0, 5).map((v) => (
          <div key={v.id} className="space-y-2 border-t border-hairline pt-4 text-body">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <span>
                {t(`fleet.${v.status}`)} · {v.cursor} / {v.node_ids.length}
              </span>
              <time className="text-label text-mute">{formatDateTime(v.created_at, i18n.language)}</time>
            </div>
            <ol className="flex flex-wrap gap-x-4 gap-y-2">
              {v.node_ids.map((id, i) => (
                <li key={id}>
                  <Link className="underline underline-offset-4" to={`/nodes/${id}?section=settings`}>
                    {i + 1}. {nodes.find((n) => n.id === id)?.name ?? id}
                  </Link>
                </li>
              ))}
            </ol>
            {v.error && <p className="text-label text-err">{v.error}</p>}
            {v.status === 'running' && isWriter && (
              <Button variant="outline" disabled={stop.isPending} onClick={() => stop.mutate(v.id)}>
                {t('fleet.stop')}
              </Button>
            )}
          </div>
        ))}
      </div>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={t('fleet.start')}
        description={t('fleet.confirm', { count: selected.length })}
        confirmLabel={t('fleet.start')}
        onConfirm={async () => {
          await start.mutateAsync();
        }}
      />
    </AdvancedSettings>
  );
}
