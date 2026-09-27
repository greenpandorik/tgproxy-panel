import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '@/lib/api';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { ErrorState } from '@/components/common/ErrorState';
import { formatDateTime } from '@/lib/format';
interface Check {
  status: 'ok' | 'failed' | 'not_run';
  latency_ms: number;
}
interface Probe {
  location: string;
  at: string;
  tls: Check;
  http: Check;
  faketls: Check;
  web: Check;
}
export function NodeProbes({ id }: { id: string }) {
  const { t, i18n } = useTranslation();
  const query = useQuery({
    queryKey: ['probes', id],
    queryFn: async () => ({
      ...(await api.get<{ items: Probe[]; expected_locations: string[] | null }>(`/api/v1/nodes/${id}/probes`)),
      observedAt: Date.now(),
    }),
    refetchInterval: 30000,
  });
  const locations = query.data?.expected_locations ?? [];
  return (
    <Panel>
      <PanelHeader title={t('probe.title')} />
      <PanelBody className="space-y-4">
        <p className="max-w-[72ch] text-body text-mute">{t('probe.hint')}</p>
        {query.isLoading ? (
          <p role="status">{t('common.loading')}</p>
        ) : query.isError ? (
          <ErrorState
            inset
            message={t('common.error_generic')}
            retryLabel={t('common.refresh')}
            onRetry={() => void query.refetch()}
          />
        ) : locations.length === 0 ? (
          <p className="text-body text-mute">{t('probe.empty')}</p>
        ) : (
          locations.map((location) => {
            const report = query.data?.items.find((p) => p.location === location);
            const stale = !report || (query.data?.observedAt ?? 0) - Date.parse(report.at) > 180000;
            return (
              <div className="border-t border-hairline pt-4" key={location}>
                <div className="flex flex-wrap justify-between gap-2 text-body font-medium">
                  <span>{location}</span>
                  <span className="text-label text-mute">
                    {stale ? t('probe.stale') : formatDateTime(report.at, i18n.language)}
                  </span>
                </div>
                <dl className="mt-2 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                  {(['tls', 'http', 'faketls', 'web'] as const).map((key) => (
                    <div key={key}>
                      <dt className="text-label text-mute">{t(`probe.${key}`)}</dt>
                      <dd className="mt-1 text-body">
                        {t(`probe.${stale ? 'not_run' : report[key].status}`)}
                        {!stale && report[key].status === 'ok' ? ` · ${report[key].latency_ms} ${t('common.ms')}` : ''}
                      </dd>
                    </div>
                  ))}
                </dl>
              </div>
            );
          })
        )}
      </PanelBody>
    </Panel>
  );
}
