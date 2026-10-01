import { useTranslation } from 'react-i18next';

import { AdvancedSettings } from '@/components/common/AdvancedSettings';
import { ErrorState } from '@/components/common/ErrorState';
import { SubSection } from '@/components/common/SubSection';
import { Skeleton } from '@/components/ui/skeleton';
import { formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import { PROBE_CHECKS, freshProbe, useNodeProbes } from './probes';

import type { ProbeCheck } from './probes';

const STATUS_DOT: Record<ProbeCheck['status'], string> = {
  ok: 'bg-ok',
  failed: 'bg-err',
  not_run: 'border border-hairline-strong',
};

function ProbeStatus({ check }: { check: ProbeCheck }) {
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

/** What the outside probes saw of the server, one location at a time. */
export function NodeProbes({ id }: { id: string }) {
  const { t, i18n } = useTranslation();
  const query = useNodeProbes(id);
  const locations = query.data?.expected_locations ?? [];
  return (
    <SubSection title={t('probe.title')}>
      {query.isLoading ? (
        <div role="status" aria-label={t('common.state.loading')} className="space-y-3">
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
          <p className="max-w-[72ch] text-body text-mute">{t('probe.none')}</p>
          <AdvancedSettings label={t('probe.setup_toggle')}>
            <p className="max-w-[72ch] text-label text-mute">{t('probe.hint')}</p>
            <p className="max-w-[72ch] text-label text-mute">{t('probe.empty')}</p>
          </AdvancedSettings>
        </>
      ) : (
        <>
          <p className="max-w-[72ch] text-label text-mute">{t('probe.hint')}</p>
          {locations.map((location) => {
            const report = freshProbe(query.data, location);
            return (
              <div className="rounded-control border border-hairline px-4 py-3" key={location}>
                <div className="flex flex-wrap items-baseline justify-between gap-2">
                  <span className="mono text-mono text-foreground">{location}</span>
                  <span className="text-label text-mute">
                    {report ? formatDateTime(report.at, i18n.language) : t('probe.stale')}
                  </span>
                </div>
                <dl className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                  {PROBE_CHECKS.map((key) => (
                    <div key={key}>
                      <dt className="text-label text-mute">{t(`probe.${key}`)}</dt>
                      <dd className="mt-1 text-body">
                        <ProbeStatus check={report ? report[key] : { status: 'not_run', latency_ms: 0 }} />
                      </dd>
                    </div>
                  ))}
                </dl>
              </div>
            );
          })}
        </>
      )}
    </SubSection>
  );
}
