import { RefreshCw } from 'lucide-react';
import { Suspense, lazy, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useBranding } from '@/api/branding';
import { MONITORING_RANGES, useNodeSeries } from '@/api/monitoring';
import { useNodeStats } from '@/api/nodes';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { SegmentedControl } from '@/components/common/SegmentedControl';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ApiError } from '@/lib/api';
import { seriesPalette } from '@/lib/chart';
import { cn } from '@/lib/utils';

import type { MonitoringRange } from '@/api/monitoring';

// recharts stays out of the shell bundle - only the chart components import it.
const LoadChart = lazy(() => import('@/pages/monitoring/LoadChart').then((m) => ({ default: m.LoadChart })));

const DEFAULT_BRAND_PRIMARY = '#3b82f6';
const DEFAULT_BRAND_ACCENT = '#22c55e';

/**
 * The node's server load over time - CPU, memory and disk - with the same
 * range switch the monitoring page uses, so a reader moving between the two
 * finds the same control in the same corner. The series is the stats
 * worker's snapshot history, so it reads while the node is offline too: the
 * question "what was it doing before it went away" is exactly the one an
 * offline node raises.
 */
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
        ) : points.length === 0 ? (
          <p className="py-8 text-center text-sm text-mute">{t('nodes.load_empty')}</p>
        ) : (
          <Suspense fallback={<Skeleton className="h-[152px] w-full" />}>
            <LoadChart points={points} colors={colors} />
          </Suspense>
        )}
      </PanelBody>
    </Panel>
  );
}

/**
 * MTProxy's own counters, printed as it reports them: raw key on the left,
 * raw value on the right, both mono, split into two columns so a couple of
 * dozen counters fit on one screen. No translation of the keys - these are the
 * names that appear in MTProxy's stats output and in every issue report about
 * it, and renaming them here would only make the two harder to match up.
 */
function NodeCountersPanel({ nodeId, online }: { nodeId: string; online: boolean }) {
  const { t } = useTranslation();
  const statsQuery = useNodeStats(nodeId, online);

  const offline = !online || (statsQuery.error instanceof ApiError && statsQuery.error.code === 'node_offline');
  const entries = statsQuery.data ? Object.entries(statsQuery.data) : [];

  return (
    <Panel>
      <PanelHeader
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
        <p className="px-4 py-6 text-center text-sm text-mute">{t('nodes.offline_message')}</p>
      ) : statsQuery.isLoading ? (
        <div className="space-y-2 p-4">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      ) : entries.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-mute">{t('nodes.stats_empty')}</p>
      ) : (
        <dl className="grid grid-cols-1 gap-px bg-hairline lg:grid-cols-2">
          {entries.map(([key, value]) => (
            <div key={key} className="flex items-baseline justify-between gap-4 bg-card px-4 py-2">
              <dt className="mono truncate text-xs text-dim">{key}</dt>
              <dd className="mono shrink-0 text-xs text-foreground">{value}</dd>
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

/** The node's statistics tab: its load history first, then the relay's own counters. */
export function NodeStatsTab({ nodeId, online }: { nodeId: string; online: boolean }) {
  return (
    <div className="space-y-4">
      <NodeLoadPanel nodeId={nodeId} />
      <NodeCountersPanel nodeId={nodeId} online={online} />
    </div>
  );
}
