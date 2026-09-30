import { Activity, ArrowDownUp, Radio, RefreshCw, TriangleAlert } from 'lucide-react';
import { Suspense, lazy, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

import { useBrandingIdentity } from '@/theme/ThemeProvider';
import { useKeyStats } from '@/api/keys';
import { EmptyState } from '@/components/common/EmptyState';
import { StatGrid } from '@/components/common/StatGrid';
import { statTone } from '@/components/common/statTone';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { seriesPalette } from '@/lib/chart';
import { formatBytes, formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';

import type { StatGridTile } from '@/components/common/StatGrid';
import type { KeyStatsRange } from '@/api/keys';
import type { KeyStatsNode } from '@/api/types';
import type { TrafficPoint } from './KeyTrafficChart';

// recharts stays out of the keys bundle - only KeyTrafficChart.tsx imports it.
const KeyTrafficChart = lazy(() => import('./KeyTrafficChart').then((m) => ({ default: m.KeyTrafficChart })));

const DEFAULT_BRAND_PRIMARY = '#0b7285';
const DEFAULT_BRAND_ACCENT = '#099268';

export function trafficDeltas(points: KeyStatsNode['points']): TrafficPoint[] {
  const out: TrafficPoint[] = [];
  for (let i = 1; i < points.length; i++) {
    const delta = points[i].total_octets - points[i - 1].total_octets;
    out.push({ t: points[i].t, bytes: delta > 0 ? delta : 0 });
  }
  return out;
}

/** The node's live connection count: the last thing it reported in the window. */
function connectionsNow(node: KeyStatsNode): number {
  return node.points.length > 0 ? node.points[node.points.length - 1].connections : 0;
}

// The pair of figures above the charts, as the panel's stat tiles.
const TOTALS_GRID = 'sm:grid-cols-2 lg:grid-cols-2';

function totalsTiles({
  connections,
  traffic,
  trafficLabel,
  connectionsLabel,
  loading,
}: {
  connections: string;
  traffic: [string, string | undefined];
  trafficLabel: string;
  connectionsLabel: string;
  loading?: boolean;
}): StatGridTile[] {
  return [
    {
      id: 'connections',
      icon: Radio,
      tone: statTone({ kind: 'stateless' }),
      label: connectionsLabel,
      value: connections,
      loading,
    },
    {
      id: 'traffic',
      icon: ArrowDownUp,
      tone: statTone({ kind: 'stateless' }),
      label: trafficLabel,
      value: traffic[0],
      unit: traffic[1],
      loading,
    },
  ];
}

function StatsSkeleton({ range }: { range: KeyStatsRange }) {
  const { t } = useTranslation();
  return (
    <div className="space-y-4">
      <StatGrid
        className={TOTALS_GRID}
        tiles={totalsTiles({
          connections: '0',
          traffic: ['0', undefined],
          connectionsLabel: t('keys.stats_connections_now'),
          trafficLabel: t(`keys.stats_traffic_range_${range}`),
          loading: true,
        })}
      />
      <div className="space-y-2">
        <Skeleton className="h-3 w-32" />
        <Skeleton className="h-[110px] w-full" />
      </div>
    </div>
  );
}

interface KeyStatsSectionProps {
  keyId: string;
  /** False when the key is bound only to tproxy nodes, which report no per-key figures. */
  hasTelemtNode: boolean;
  range: KeyStatsRange;
}

export function KeyStatsSection({ keyId, hasTelemtNode, range }: KeyStatsSectionProps) {
  const { t, i18n } = useTranslation();
  const statsQuery = useKeyStats(keyId, range, hasTelemtNode);
  const { branding } = useBrandingIdentity();

  const nodes = statsQuery.data?.nodes ?? [];
  const totals = statsQuery.data?.totals;

  // One colour for every node's chart.
  const color = useMemo(
    () => seriesPalette(branding?.primary_color || DEFAULT_BRAND_PRIMARY, branding?.accent_color || DEFAULT_BRAND_ACCENT, 1)[0],
    [branding?.primary_color, branding?.accent_color],
  );

  const traffic = formatBytes(totals?.octets_delta ?? 0).split(' ');

  return (
    <section className="space-y-4">
      {!hasTelemtNode ? (
        <EmptyState className="py-8" icon={Activity} title={t('keys.stats_unavailable')} />
      ) : statsQuery.isLoading ? (
        <StatsSkeleton range={range} />
      ) : statsQuery.isError ? (
        <EmptyState
          className="py-8"
          icon={TriangleAlert}
          title={t('common.error_generic')}
          action={
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={statsQuery.isFetching}
              onClick={() => void statsQuery.refetch()}
            >
              <RefreshCw className={cn(statsQuery.isFetching && 'animate-spin')} />
              {t('common.refresh')}
            </Button>
          }
        />
      ) : nodes.length === 0 ? (
        <EmptyState className="py-8" icon={Activity} title={t('keys.stats_empty')} />
      ) : (
        <>
          <StatGrid
            className={TOTALS_GRID}
            tiles={totalsTiles({
              connections: formatNumber(totals?.connections_now ?? 0, i18n.language),
              traffic: [traffic[0], traffic[1]],
              connectionsLabel: t('keys.stats_connections_now'),
              trafficLabel: t(`keys.stats_traffic_range_${range}`),
            })}
          />

          <div className="space-y-4">
            {nodes.map((node) => {
              const points = trafficDeltas(node.points);
              return (
                <div key={node.node_id} className="space-y-2">
                  <div className="flex items-baseline justify-between gap-3">
                    <span className="truncate text-label text-foreground">{node.node_name}</span>
                    <span className="mono shrink-0 text-mono text-mute">
                      {t('keys.stats_node_connections', { count: connectionsNow(node) })}
                    </span>
                  </div>
                  {points.length < 2 ? (
                    <p className="flex h-[110px] items-center justify-center rounded-control border border-dashed border-hairline px-4 text-center text-label text-mute">
                      {t('keys.stats_chart_empty')}
                    </p>
                  ) : (
                    <Suspense fallback={<Skeleton className="h-[110px] w-full" />}>
                      <KeyTrafficChart points={points} color={color} />
                    </Suspense>
                  )}
                </div>
              );
            })}
          </div>
        </>
      )}
    </section>
  );
}
