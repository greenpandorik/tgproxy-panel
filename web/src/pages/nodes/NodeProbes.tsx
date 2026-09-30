import { useQuery } from '@tanstack/react-query';
import { Earth } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { AdvancedSettings } from '@/components/common/AdvancedSettings';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Skeleton } from '@/components/ui/skeleton';
import { api } from '@/lib/api';
import { formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

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

const PROBE_CHECKS = ['tls', 'http', 'faketls', 'web'] as const;

const STATUS_DOT: Record<Check['status'], string> = {
  ok: 'bg-ok',
  failed: 'bg-err',
  not_run: 'border border-hairline-strong',
};

function ProbeStatus({ check }: { check: Check }) {
  const { t } = useTranslation();
  return (
    <span className="inline-flex items-center gap-2">
      <span className={cn('size-[7px] shrink-0 rounded-pill', STATUS_DOT[check.status])} aria-hidden="true" />
      <span className={check.status === 'failed' ? 'text-err' : check.status === 'ok' ? 'text-foreground' : 'text-mute'}>
        {t(`probe.${check.status}`)}
      </span>
      {check.status === 'ok' && (
        <span className="mono text-mono text-mute">
          {check.latency_ms} {t('common.ms')}
        </span>
      )}
    </span>
  );
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
      <PanelHeader icon={Earth} title={t('probe.title')} />
      <PanelBody className="space-y-4">
        {locations.length > 0 && <p className="max-w-[72ch] text-body text-mute">{t('probe.hint')}</p>}
        {query.isLoading ? (
          <div role="status" aria-label={t('common.loading')} className="space-y-3">
            <Skeleton className="h-3 w-40" />
            <Skeleton className="h-3 w-full" />
          </div>
        ) : query.isError ? (
          <ErrorState
            inset
            message={t('common.error_generic')}
            retryLabel={t('common.refresh')}
            onRetry={() => void query.refetch()}
          />
        ) : locations.length === 0 ? (
          <>
            <p className="text-body text-mute">{t('probe.none')}</p>
            <AdvancedSettings label={t('probe.setup_toggle')}>
              <p className="max-w-[72ch] text-label text-mute">{t('probe.hint')}</p>
              <p className="max-w-[72ch] text-label text-mute">{t('probe.empty')}</p>
            </AdvancedSettings>
          </>
        ) : (
          locations.map((location) => {
            const report = query.data?.items.find((p) => p.location === location);
            const stale = !report || (query.data?.observedAt ?? 0) - Date.parse(report.at) > 180000;
            return (
              <div className="border-t border-hairline pt-4" key={location}>
                <div className="flex flex-wrap items-baseline justify-between gap-2">
                  <span className="mono text-mono text-foreground">{location}</span>
                  <span className="text-label text-mute">
                    {stale ? t('probe.stale') : formatDateTime(report.at, i18n.language)}
                  </span>
                </div>
                <dl className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                  {PROBE_CHECKS.map((key) => (
                    <div key={key}>
                      <dt className="text-label text-mute">{t(`probe.${key}`)}</dt>
                      <dd className="mt-1 text-body">
                        <ProbeStatus check={stale ? { status: 'not_run', latency_ms: 0 } : report[key]} />
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
