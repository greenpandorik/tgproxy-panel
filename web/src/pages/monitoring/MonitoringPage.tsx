import { Suspense, lazy, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';

import { useBranding } from '@/api/branding';
import { useMonitoringOverview } from '@/api/monitoring';
import { useNodes } from '@/api/nodes';
import { CopyButton } from '@/components/common/CopyButton';
import { EmptyState } from '@/components/common/EmptyState';
import { PageHeader } from '@/components/common/PageHeader';
import { SegmentedControl } from '@/components/common/SegmentedControl';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { StatusBadge } from '@/components/common/StatusBadge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { seriesPalette } from '@/lib/chart';

import type { MonitoringRange } from '@/api/monitoring';
import type { Status } from '@/components/common/StatusBadge';
import type { MonitoringNode, MonitoringPoint, NodeEngine } from '@/api/types';

// recharts stays out of the shell bundle - only NodeSeriesChart.tsx imports it.
const NodeSeriesChart = lazy(() => import('./NodeSeriesChart').then((m) => ({ default: m.NodeSeriesChart })));

const RANGES: MonitoringRange[] = ['1h', '6h', '24h', '7d'];

const DEFAULT_BRAND_PRIMARY = '#3b82f6';
const DEFAULT_BRAND_ACCENT = '#22c55e';

const METRICS_SNIPPET = `scrape_configs:
  - job_name: tgwp-panel
    scheme: https
    scrape_interval: 60s
    authorization: { credentials_file: /etc/prometheus/tgwp-token }
    static_configs: [{ targets: ['<panel host>'] }]`;

function ChartSkeleton() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-[152px] w-full" />
      <Skeleton className="h-[152px] w-full" />
    </div>
  );
}

function NodeCard({
  node,
  points,
  colors,
  engine,
}: {
  node: MonitoringNode;
  points: MonitoringPoint[];
  colors: [string, string];
  engine: NodeEngine;
}) {
  const { t } = useTranslation();

  return (
    <Panel>
      <div className="flex h-10 items-center justify-between gap-3 border-b border-hairline px-4">
        <div className="flex min-w-0 items-center gap-2">
          <StatusBadge status={node.status as Status} hideLabel />
          <h2 className="truncate text-sm font-medium text-foreground">{node.node_name}</h2>
          <span className="mono truncate text-xs text-dim">{node.hostname}</span>
        </div>
        <Link to={`/nodes/${node.node_id}`} className="shrink-0 text-xs text-primary hover:underline">
          {t('monitoring.open_node')}
        </Link>
      </div>

      <PanelBody>
        {points.length === 0 ? (
          <p className="py-8 text-center text-sm text-mute">{t('monitoring.node_empty')}</p>
        ) : (
          <Suspense fallback={<ChartSkeleton />}>
            <NodeSeriesChart points={points} colors={colors} engine={engine} />
          </Suspense>
        )}
      </PanelBody>
    </Panel>
  );
}

function NodeCardSkeleton() {
  return (
    <Panel>
      <div className="flex h-10 items-center gap-3 border-b border-hairline px-4">
        <Skeleton className="h-4 w-28" />
        <Skeleton className="h-3 w-40" />
      </div>
      <PanelBody>
        <ChartSkeleton />
      </PanelBody>
    </Panel>
  );
}

export function MonitoringPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [range, setRange] = useState<MonitoringRange>('24h');
  const overviewQuery = useMonitoringOverview(range);
  const { data: branding } = useBranding();
  // The monitoring payload carries no engine, and the engine decides how many
  // series a node's readings are; the node list already in cache supplies it.
  const nodesQuery = useNodes();
  const engineById = new Map((nodesQuery.data?.items ?? []).map((n) => [n.id, n.engine]));

  const nodes = overviewQuery.data?.nodes ?? [];
  const series = overviewQuery.data?.series ?? {};
  const loading = overviewQuery.isLoading;

  // Every card uses the same two colours: within a card the legend says which
  // line is which, and across cards a shared pair means the reader learns the
  // shape once instead of re-reading a legend per node.
  const colors = useMemo((): [string, string] => {
    const [first, second] = seriesPalette(
      branding?.primary_color || DEFAULT_BRAND_PRIMARY,
      branding?.accent_color || DEFAULT_BRAND_ACCENT,
      2,
    );
    return [first, second];
  }, [branding?.primary_color, branding?.accent_color]);

  return (
    <>
      <PageHeader
        title={t('monitoring.title')}
        actions={
          <SegmentedControl
            label={t('monitoring.range_label')}
            value={range}
            onChange={setRange}
            options={RANGES.map((r) => ({ value: r, label: t(`monitoring.range_${r}`) }))}
          />
        }
      />

      {loading ? (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <NodeCardSkeleton />
          <NodeCardSkeleton />
        </div>
      ) : nodes.length === 0 ? (
        <EmptyState
          title={t('monitoring.empty_no_nodes')}
          action={
            <Button type="button" onClick={() => navigate('/nodes')}>
              {t('nodes.add')}
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          {nodes.map((node) => (
            <NodeCard
              key={node.node_id}
              node={node}
              points={series[node.node_id] ?? []}
              colors={colors}
              engine={engineById.get(node.node_id) ?? 'tproxy'}
            />
          ))}
        </div>
      )}

      <Panel>
        <PanelHeader title={t('monitoring.prometheus_title')} actions={<CopyButton value={METRICS_SNIPPET} />} />
        <PanelBody className="space-y-3 p-4">
          <p className="max-w-[72ch] text-sm text-mute">{t('monitoring.prometheus_description')}</p>
          <pre className="mono overflow-x-auto rounded-md border border-hairline bg-background px-3 py-2.5 text-xs leading-relaxed text-foreground">
            {METRICS_SNIPPET}
          </pre>
          <p className="max-w-[72ch] text-xs text-mute">{t('monitoring.prometheus_node_note')}</p>
          <p className="text-xs text-dim">
            {t('monitoring.docs_link_prefix')} <span className="mono">docs/monitoring.md</span>
          </p>
        </PanelBody>
      </Panel>
    </>
  );
}
