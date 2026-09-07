import { RefreshCw } from 'lucide-react';
import { Suspense, lazy, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useBranding } from '@/api/branding';
import { useKeyStats } from '@/api/keys';
import { EmptyState } from '@/components/common/EmptyState';
import { SegmentedControl } from '@/components/common/SegmentedControl';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { seriesPalette } from '@/lib/chart';
import { formatBytes, formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';

import type { KeyStatsRange } from '@/api/keys';
import type { KeyStatsNode } from '@/api/types';
import type { TrafficPoint } from './KeyTrafficChart';

// recharts stays out of the keys bundle - only KeyTrafficChart.tsx imports it.
const KeyTrafficChart = lazy(() => import('./KeyTrafficChart').then((m) => ({ default: m.KeyTrafficChart })));

const RANGES: KeyStatsRange[] = ['24h', '7d'];

const DEFAULT_BRAND_PRIMARY = '#3b82f6';
const DEFAULT_BRAND_ACCENT = '#22c55e';

/**
 * telemt's octet counter is cumulative and resets when the process restarts, so
 * the traffic in an interval is the rise between two samples and a fall is a
 * restart, not negative traffic. The backend takes the same view for its totals.
 */
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

/** One figure of the pair above the charts: the number large, what it counts under it. */
function Total({ label, value, unit }: { label: string; value: string; unit?: string }) {
  return (
    <div className="min-w-0">
      <p className="mono text-title text-foreground">
        {value}
        {unit && <span className="ml-1 text-label text-mute">{unit}</span>}
      </p>
      <p className="mt-1 truncate text-label text-mute">{label}</p>
    </div>
  );
}

/**
 * The silhouette of what is loading: the totals tile with its two figures, then
 * one node's caption and plot. A generic bar would tell the operator something
 * is coming; this tells them what, so nothing jumps when it lands.
 */
function StatsSkeleton() {
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-4 rounded-surface border border-hairline px-4 py-3">
        {[0, 1].map((i) => (
          <div key={i} className="space-y-2">
            <Skeleton className="h-6 w-20" />
            <Skeleton className="h-3 w-28" />
          </div>
        ))}
      </div>
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
}

/**
 * What the key actually did: how much traffic it moved and how many connections
 * it is holding open, per node, over the last day or week.
 *
 * The two totals come first because they answer the question that opened the
 * drawer ("is this key being used, and how hard"); the per-node charts under them
 * answer "when". Only telemt reports any of this, so a key on tproxy nodes gets
 * one sentence saying why the block is empty rather than an empty chart.
 */
export function KeyStatsSection({ keyId, hasTelemtNode }: KeyStatsSectionProps) {
  const { t, i18n } = useTranslation();
  const [range, setRange] = useState<KeyStatsRange>('24h');
  const statsQuery = useKeyStats(keyId, range, hasTelemtNode);
  const { data: branding } = useBranding();

  const nodes = statsQuery.data?.nodes ?? [];
  const totals = statsQuery.data?.totals;

  /*
   * One colour for every node's chart. These are small multiples, not series in
   * one plot: identity is carried by the node's name above each chart, so giving
   * each its own hue would spend colour on a distinction nothing has to make -
   * and would run out on the sixth node.
   */
  const color = useMemo(
    () => seriesPalette(branding?.primary_color || DEFAULT_BRAND_PRIMARY, branding?.accent_color || DEFAULT_BRAND_ACCENT, 1)[0],
    [branding?.primary_color, branding?.accent_color],
  );

  const traffic = formatBytes(totals?.octets_delta ?? 0).split(' ');

  return (
    <section className="space-y-4 border-t border-hairline pt-4">
      <div className="flex items-center justify-between gap-3">
        <h3 className="micro text-mute">{t('keys.stats_title')}</h3>
        {hasTelemtNode && (
          <SegmentedControl
            label={t('keys.stats_range_label')}
            value={range}
            onChange={setRange}
            options={RANGES.map((r) => ({ value: r, label: t(`keys.stats_range_${r}`) }))}
          />
        )}
      </div>

      {!hasTelemtNode ? (
        <EmptyState className="py-8" title={t('keys.stats_unavailable')} />
      ) : statsQuery.isLoading ? (
        <StatsSkeleton />
      ) : statsQuery.isError ? (
        <EmptyState
          className="py-8"
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
        <EmptyState className="py-8" title={t('keys.stats_empty')} />
      ) : (
        <>
          <div className="grid grid-cols-2 gap-4 rounded-surface border border-hairline px-4 py-3">
            <Total label={t('keys.stats_connections_now')} value={formatNumber(totals?.connections_now ?? 0, i18n.language)} />
            <Total label={t(`keys.stats_traffic_range_${range}`)} value={traffic[0]} unit={traffic[1]} />
          </div>

          <div className="space-y-4">
            {nodes.map((node) => (
              <div key={node.node_id}>
                <div className="flex items-baseline justify-between gap-3">
                  <span className="truncate text-label text-foreground">{node.node_name}</span>
                  <span className="mono shrink-0 text-mono text-dim">
                    {t('keys.stats_node_connections', { count: connectionsNow(node) })}
                  </span>
                </div>
                <Suspense fallback={<Skeleton className="mt-2 h-[110px] w-full" />}>
                  <KeyTrafficChart points={trafficDeltas(node.points)} color={color} />
                </Suspense>
              </div>
            ))}
          </div>
        </>
      )}
    </section>
  );
}
