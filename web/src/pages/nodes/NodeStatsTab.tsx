import { Gauge, Globe, RefreshCw, Sigma } from 'lucide-react';
import { Suspense, lazy, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useBranding } from '@/api/branding';
import { MONITORING_RANGES, useNodeSeries } from '@/api/monitoring';
import { useNodeStats } from '@/api/nodes';
import { PanelEmpty } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { SegmentedControl } from '@/components/common/SegmentedControl';
import { Button } from '@/components/ui/button';
import { ENTER_CLASS } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { ApiError } from '@/lib/api';
import { seriesPalette } from '@/lib/chart';
import { cn } from '@/lib/utils';

import { dcLatencyRows } from './dcDisplay';

import type { MonitoringRange } from '@/api/monitoring';
import type { NodeEngine } from '@/api/types';

// recharts stays out of the shell bundle - only the chart components import it.
const LoadChart = lazy(() => import('@/pages/monitoring/LoadChart').then((m) => ({ default: m.LoadChart })));
const DcLatencyChart = lazy(() => import('./DcLatencyChart').then((m) => ({ default: m.DcLatencyChart })));

const DEFAULT_BRAND_PRIMARY = '#3b82f6';
const DEFAULT_BRAND_ACCENT = '#22c55e';

function NodeLoadPanel({ nodeId }: { nodeId: string }) {
  const { t } = useTranslation();
  const [range, setRange] = useState<MonitoringRange>('24h');
  const seriesQuery = useNodeSeries(nodeId, range);
  const { data: branding } = useBranding();

  const colors = useMemo((): [string, string, string] => {
    const [first, second, third] = seriesPalette(
      branding?.primary_color || DEFAULT_BRAND_PRIMARY,
      branding?.accent_color || DEFAULT_BRAND_ACCENT,
      3,
    );
    return [first, second, third];
  }, [branding?.primary_color, branding?.accent_color]);

  const points = seriesQuery.data?.points ?? [];

  return (
    <Panel>
      <PanelHeader
        icon={Gauge}
        title={t('nodes.load_title')}
        actions={
          <SegmentedControl
            label={t('monitoring.range_label')}
            value={range}
            onChange={setRange}
            options={MONITORING_RANGES.map((r) => ({ value: r, label: t(`monitoring.range_${r}`) }))}
          />
        }
      />
      <PanelBody>
        {seriesQuery.isLoading ? (
          <Skeleton className="h-[152px] w-full" />
        ) : seriesQuery.isError ? (
          <ErrorState
            inset
            message={t('common.error_generic')}
            retryLabel={t('common.refresh')}
            onRetry={() => void seriesQuery.refetch()}
          />
        ) : points.length === 0 ? (
          <PanelEmpty>{t('nodes.load_empty')}</PanelEmpty>
        ) : (
          <Suspense fallback={<Skeleton className="h-[152px] w-full" />}>
            <LoadChart points={points} colors={colors} />
          </Suspense>
        )}
      </PanelBody>
    </Panel>
  );
}

function NodeDcLatencyPanel({ nodeId, engine }: { nodeId: string; engine: NodeEngine }) {
  const { t } = useTranslation();
  const [range, setRange] = useState<MonitoringRange>('24h');
  const telemt = engine === 'telemt';
  // An empty id disables the query, which is exactly what a tproxy node wants.
  const seriesQuery = useNodeSeries(telemt ? nodeId : '', range);

  const chart = useMemo(() => dcLatencyRows(seriesQuery.data?.points ?? []), [seriesQuery.data]);

  return (
    <Panel>
      <PanelHeader
        icon={Globe}
        title={t('nodes.dc_latency_title')}
        actions={
          telemt && (
            <SegmentedControl
              label={t('monitoring.range_label')}
              value={range}
              onChange={setRange}
              options={MONITORING_RANGES.map((r) => ({ value: r, label: t(`monitoring.range_${r}`) }))}
            />
          )
        }
      />
      <PanelBody>
        {!telemt ? (
          <PanelEmpty>{t('nodes.dcs_unavailable_engine')}</PanelEmpty>
        ) : seriesQuery.isLoading ? (
          <Skeleton className="h-[152px] w-full" />
        ) : seriesQuery.isError ? (
          <ErrorState
            inset
            message={t('common.error_generic')}
            retryLabel={t('common.refresh')}
            onRetry={() => void seriesQuery.refetch()}
          />
        ) : chart.rows.length === 0 ? (
          <PanelEmpty>{t('nodes.dc_latency_empty')}</PanelEmpty>
        ) : (
          <Suspense fallback={<Skeleton className="h-[152px] w-full" />}>
            <DcLatencyChart dcs={chart.dcs} rows={chart.rows} />
          </Suspense>
        )}
      </PanelBody>
    </Panel>
  );
}

function NodeCountersPanel({ nodeId, online }: { nodeId: string; online: boolean }) {
  const { t } = useTranslation();
  const statsQuery = useNodeStats(nodeId, online);

  const offline = !online || (statsQuery.error instanceof ApiError && statsQuery.error.code === 'node_offline');
  const entries = statsQuery.data ? Object.entries(statsQuery.data) : [];

  return (
    <Panel>
      <PanelHeader
        icon={Sigma}
        title={t('nodes.stats_title')}
        meta={entries.length > 0 ? String(entries.length) : undefined}
        actions={
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => void statsQuery.refetch()}
            disabled={offline || statsQuery.isFetching}
          >
            <RefreshCw className={cn(statsQuery.isFetching && 'animate-spin')} />
            {t('common.refresh')}
          </Button>
        }
      />

      {offline ? (
        <PanelEmpty>{t('nodes.offline_message')}</PanelEmpty>
      ) : statsQuery.isLoading ? (
        <div className="grid grid-cols-1 gap-px bg-hairline lg:grid-cols-2">
          {Array.from({ length: 8 }).map((_, i) => (
            <div key={i} className="flex items-center justify-between gap-4 bg-card px-4 py-2">
              <Skeleton className="h-3 w-40" />
              <Skeleton className="h-3 w-12" />
            </div>
          ))}
        </div>
      ) : statsQuery.isError ? (
        <ErrorState
          inset
          message={t('common.error_generic')}
          retryLabel={t('common.refresh')}
          onRetry={() => void statsQuery.refetch()}
        />
      ) : entries.length === 0 ? (
        <PanelEmpty>{t('nodes.stats_empty')}</PanelEmpty>
      ) : (
        <dl className="grid grid-cols-1 gap-px bg-hairline lg:grid-cols-2">
          {entries.map(([key, value]) => (
            <div key={key} className="flex items-baseline justify-between gap-4 bg-card px-4 py-2">
              <dt className="mono truncate text-mono text-mute">{key}</dt>
              <dd className="mono shrink-0 text-mono text-foreground">{value}</dd>
            </div>
          ))}
          {/* An odd counter count would leave the grid's last slot empty, and the
              1px gaps are painted by the container - so the hole would show up as
              a lit rectangle. Fill it with the panel surface instead. */}
          {entries.length % 2 === 1 && <div className="hidden bg-card lg:block" aria-hidden="true" />}
        </dl>
      )}
    </Panel>
  );
}

/** The node's statistics tab: its load history, its latency to Telegram, then the relay's own counters. */
export function NodeStatsTab({ nodeId, online, engine }: { nodeId: string; online: boolean; engine: NodeEngine }) {
  return (
    <div className={cn(ENTER_CLASS, 'space-y-4')}>
      <NodeLoadPanel nodeId={nodeId} />
      <NodeDcLatencyPanel nodeId={nodeId} engine={engine} />
      <NodeCountersPanel nodeId={nodeId} online={online} />
    </div>
  );
}
