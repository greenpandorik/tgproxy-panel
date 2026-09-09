import { Plug, Server } from 'lucide-react';
import { Suspense, lazy, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';

import { useBranding } from '@/api/branding';
import { MONITORING_RANGES, useMonitoringOverview } from '@/api/monitoring';
import { useNodes } from '@/api/nodes';
import { CopyButton } from '@/components/common/CopyButton';
import { EmptyState, PanelEmpty } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { PageHeader } from '@/components/common/PageHeader';
import { SegmentedControl } from '@/components/common/SegmentedControl';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { StatusBadge } from '@/components/common/StatusBadge';
import { Button } from '@/components/ui/button';
import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { seriesPalette } from '@/lib/chart';

import type { ReactNode } from 'react';
import type { MonitoringRange } from '@/api/monitoring';
import type { Status } from '@/components/common/StatusBadge';
import type { MonitoringNode, MonitoringPoint, NodeEngine } from '@/api/types';

// recharts stays out of the shell bundle - only NodeSeriesChart.tsx imports it.
const NodeSeriesChart = lazy(() => import('./NodeSeriesChart').then((m) => ({ default: m.NodeSeriesChart })));

const DEFAULT_BRAND_PRIMARY = '#3b82f6';
const DEFAULT_BRAND_ACCENT = '#22c55e';

const METRICS_SNIPPET = `scrape_configs:
  - job_name: tgwp-panel
    scheme: https
    scrape_interval: 60s
    authorization: { credentials_file: /etc/prometheus/tgwp-token }
    static_configs: [{ targets: ['<panel host>'] }]`;

function Arriving({ index, children }: { index: number; children: ReactNode }) {
  return (
    <div className={ENTER_CLASS} style={enterDelay(index)}>
      {children}
    </div>
  );
}

/** The three stacked charts in silhouette: a plot area and the legend line under it, three times. */
function ChartSkeleton() {
  return (
    <div className="space-y-4">
      {[0, 1, 2].map((i) => (
        <div key={i} className="space-y-1.5">
          <Skeleton className="h-35 w-full" />
          <Skeleton className="h-3 w-40" />
        </div>
      ))}
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
  colors: [string, string, string];
  engine: NodeEngine;
}) {
  const { t } = useTranslation();

  return (
    <Panel>
      {/* Same head as every PanelHeader in the panel, with the node's state dot
          and hostname taking the place of the mono note. */}
      <div className="flex min-h-11 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b border-hairline px-4 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <StatusBadge status={node.status as Status} hideLabel />
          <h2 className="truncate text-title text-foreground">{node.node_name}</h2>
          <span className="mono truncate text-mono text-mute">{node.hostname}</span>
        </div>
        <Link to={`/nodes/${node.node_id}`} className="shrink-0 text-label text-brand-ink hover:underline">
          {t('monitoring.open_node')}
        </Link>
      </div>

      <PanelBody>
        {points.length === 0 ? (
          <PanelEmpty>{t('monitoring.node_empty')}</PanelEmpty>
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
      <div className="flex min-h-11 items-center gap-3 border-b border-hairline px-4 py-2">
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
  const nodesQuery = useNodes();
  const engineById = new Map((nodesQuery.data?.items ?? []).map((n) => [n.id, n.engine]));

  const nodes = overviewQuery.data?.nodes ?? [];
  const series = overviewQuery.data?.series ?? {};
  const loading = overviewQuery.isLoading;

  const colors = useMemo((): [string, string, string] => {
    const [first, second, third] = seriesPalette(
      branding?.primary_color || DEFAULT_BRAND_PRIMARY,
      branding?.accent_color || DEFAULT_BRAND_ACCENT,
      3,
    );
    return [first, second, third];
  }, [branding?.primary_color, branding?.accent_color]);

  return (
    <>
      <PageHeader
        title={t('monitoring.title')}
        actions={
          <>
            <HelpButton topic="monitoring" />
            <SegmentedControl
              label={t('monitoring.range_label')}
              value={range}
              onChange={setRange}
              options={MONITORING_RANGES.map((r) => ({ value: r, label: t(`monitoring.range_${r}`) }))}
            />
          </>
        }
      />

      {loading ? (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <NodeCardSkeleton />
          <NodeCardSkeleton />
        </div>
      ) : overviewQuery.isError ? (
        /* The readings failed to arrive, which is not the same as a network with
           no nodes in it - so it says so, and offers the one useful move. */
        <ErrorState
          message={overviewQuery.error instanceof ApiError ? overviewQuery.error.message : t('common.error_generic')}
          retryLabel={t('common.refresh')}
          onRetry={() => void overviewQuery.refetch()}
        />
      ) : nodes.length === 0 ? (
        <EmptyState
          icon={Server}
          title={t('monitoring.empty_no_nodes')}
          action={
            <Button type="button" onClick={() => navigate('/nodes')}>
              {t('nodes.add')}
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          {nodes.map((node, i) => (
            <Arriving key={node.node_id} index={i}>
              <NodeCard
                node={node}
                points={series[node.node_id] ?? []}
                colors={colors}
                engine={engineById.get(node.node_id) ?? 'tproxy'}
              />
            </Arriving>
          ))}
        </div>
      )}

      <Arriving index={nodes.length}>
        <Panel>
          <PanelHeader icon={Plug} title={t('monitoring.prometheus_title')} actions={<CopyButton value={METRICS_SNIPPET} />} />
          <PanelBody className="space-y-4">
            <p className="max-w-[72ch] text-body text-mute">{t('monitoring.prometheus_description')}</p>
            <pre className="mono overflow-x-auto rounded-control border border-hairline bg-background px-3 py-2.5 text-mono text-foreground">
              {METRICS_SNIPPET}
            </pre>
            <p className="max-w-[72ch] text-label text-mute">{t('monitoring.prometheus_node_note')}</p>
            <p className="text-label text-mute">
              {t('monitoring.docs_link_prefix')} <span className="mono text-mono">docs/monitoring.md</span>
            </p>
          </PanelBody>
        </Panel>
      </Arriving>
    </>
  );
}
