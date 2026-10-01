import { RefreshCw } from 'lucide-react';
import { Suspense, lazy, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

import { useNodeStats } from '@/api/nodes';
import { AdvancedSettings } from '@/components/common/AdvancedSettings';
import { ErrorState } from '@/components/common/ErrorState';
import { MetricValue } from '@/components/common/MetricValue';
import { SubSection, SubSections } from '@/components/common/SubSection';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ApiError } from '@/lib/api';
import { formatCompactDuration } from '@/lib/format';
import { cn } from '@/lib/utils';

import { ReliabilityResources } from './NodeReliability';
import { usageTone } from './nodeHealth';

import type { Node, NodeHealth, SeriesPoint } from '@/api/types';
import type { ReactNode } from 'react';

const LoadChart = lazy(() => import('@/pages/monitoring/LoadChart').then((m) => ({ default: m.LoadChart })));

const TONE_TEXT = { neutral: 'text-mute', ok: 'text-mute', warn: 'text-warn', err: 'text-err' } as const;

function Cell({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 bg-card px-4 py-3">
      <span className="text-label text-mute">{label}</span>
      <span className="mono shrink-0 text-mono">{children}</span>
    </div>
  );
}

function ServiceState({ active }: { active: boolean }) {
  const { t } = useTranslation();
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={cn('size-[7px] shrink-0 rounded-pill', active ? 'bg-ok' : 'bg-err')} aria-hidden="true" />
      <span className={active ? 'text-mute' : 'text-err'}>{t(active ? 'nodes.state_up' : 'nodes.state_down')}</span>
    </span>
  );
}

const GRID = 'grid grid-cols-1 gap-px overflow-hidden rounded-control border border-hairline bg-hairline sm:grid-cols-2';

function Filler({ count }: { count: number }) {
  return count % 2 === 1 ? <div className="hidden bg-card sm:block" aria-hidden="true" /> : null;
}

function Counters({ node }: { node: Node }) {
  const { t } = useTranslation();
  const statsQuery = useNodeStats(node.id, node.online);
  const entries = statsQuery.data ? Object.entries(statsQuery.data) : [];
  return (
    <SubSection
      title={t('nodes.stats_title')}
      meta={entries.length > 0 ? String(entries.length) : undefined}
      actions={
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => void statsQuery.refetch()}
          disabled={statsQuery.isFetching}
        >
          <RefreshCw className={cn(statsQuery.isFetching && 'animate-spin')} />
          {t('common.refresh')}
        </Button>
      }
    >
      {statsQuery.isLoading ? (
        <Skeleton className="h-16 w-full" />
      ) : statsQuery.isError ? (
        <ErrorState
          inset
          message={statsQuery.error instanceof ApiError ? statsQuery.error.message : t('common.error_generic')}
          retryLabel={t('common.refresh')}
          onRetry={() => void statsQuery.refetch()}
        />
      ) : entries.length === 0 ? (
        <p className="text-body text-mute">{t('nodes.stats_empty')}</p>
      ) : (
        <AdvancedSettings label={t('nodes.detail_counters_show', { count: entries.length })}>
          <dl className="grid grid-cols-1 gap-px overflow-hidden rounded-control border border-hairline bg-hairline lg:grid-cols-2">
            {entries.map(([key, value]) => (
              <div key={key} className="flex items-baseline justify-between gap-4 bg-card px-4 py-2">
                <dt className="mono truncate text-mono text-mute">{key}</dt>
                <dd className="mono shrink-0 text-mono text-foreground">{value}</dd>
              </div>
            ))}
            {entries.length % 2 === 1 && <div className="hidden bg-card lg:block" aria-hidden="true" />}
          </dl>
        </AdvancedSettings>
      )}
    </SubSection>
  );
}

/** The services on the server and its resources: what runs, what is left, and how both changed. */
export function NodeServicesSection({
  node,
  health,
  offline,
  error,
  onRetry,
  points,
  rangeLabel,
}: {
  node: Node;
  health?: NodeHealth;
  offline: boolean;
  error: boolean;
  onRetry: () => void;
  points: SeriesPoint[];
  rangeLabel: string;
}) {
  const { t, i18n } = useTranslation();
  const telemt = node.engine === 'telemt';
  const history = useMemo(() => points.map((p) => ({ ...p, cpu_percent: p.cpu_utilisation_percent ?? null })), [points]);
  const pct = (v: number) => `${v.toFixed(0)}%`;

  if (offline) return <p className="pt-2 text-body text-mute">{t('nodes.offline_message')}</p>;
  if (error) return <ErrorState inset message={t('common.error_generic')} retryLabel={t('common.refresh')} onRetry={onRetry} />;
  if (!health) return <Skeleton className="mt-2 h-32 w-full" />;

  const services = [
    { label: t(telemt ? 'nodes.overview_telemt_active' : 'nodes.overview_relay_active'), active: health.relay_active },
    ...(telemt ? [] : [{ label: t('nodes.overview_mtproxy_active'), active: health.mtproxy_active }]),
    { label: t('nodes.overview_caddy_active'), active: health.caddy_active },
    { label: t('nodes.overview_healthz'), active: health.healthz },
    { label: t('nodes.overview_readyz'), active: health.readyz },
  ];

  return (
    <SubSections>
      <SubSection title={t('nodes.overview_health')}>
        <div className={GRID}>
          {services.map((s) => (
            <Cell key={s.label} label={s.label}>
              <ServiceState active={s.active} />
            </Cell>
          ))}
          <Filler count={services.length} />
        </div>
      </SubSection>
      <SubSection title={t('nodes.detail_resources')}>
        <div className={GRID}>
          <Cell label={t('nodes.overview_uptime')}>
            <span className="text-mute">{formatCompactDuration(health.uptime_seconds, i18n.language)}</span>
          </Cell>
          <Cell label={t('nodes.detail_load_average')}>
            <MetricValue value={health.load_average_1} format={(v) => v.toFixed(2)} className="text-mute" />
          </Cell>
          <Cell label={t('nodes.overview_mem')}>
            <span className={TONE_TEXT[usageTone(health.mem_used_percent)]}>{pct(health.mem_used_percent)}</span>
          </Cell>
          <Cell label={t('nodes.overview_disk')}>
            <span className={TONE_TEXT[usageTone(health.disk_used_percent)]}>{pct(health.disk_used_percent)}</span>
          </Cell>
        </div>
        {history.length > 0 && (
          <div className="pt-2">
            <p className="mb-2 text-label text-mute">{t('nodes.detail_resources_history', { range: rangeLabel })}</p>
            <Suspense fallback={<Skeleton className="h-[152px] w-full" />}>
              <LoadChart points={history} colors={['var(--series-1)', 'var(--series-2)', 'var(--series-3)']} />
            </Suspense>
          </div>
        )}
      </SubSection>
      <ReliabilityResources report={health.reliability} />
      <Counters node={node} />
    </SubSections>
  );
}
