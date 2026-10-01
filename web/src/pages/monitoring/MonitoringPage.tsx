import { Server } from 'lucide-react';
import { Suspense, lazy, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom';

import { useBrandingIdentity } from '@/theme/ThemeProvider';
import { MONITORING_RANGES, useMonitoringOverview } from '@/api/monitoring';
import { useFleetRollouts, useNodes } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { EmptyState, PanelEmpty } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { PageHeader } from '@/components/common/PageHeader';
import { SectionTabs, useSection } from '@/components/common/SectionNav';
import { SegmentedControl } from '@/components/common/SegmentedControl';
import { Panel, PanelBody } from '@/components/common/Panel';
import { StatusBadge } from '@/components/common/StatusBadge';
import { Button } from '@/components/ui/button';
import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { seriesPalette } from '@/lib/chart';

import { FleetCarriersCard } from './FleetCarriersCard';
import { FleetSummary } from './FleetSummary';
import { inServerOrder } from './fleetFacts';
import { ServerStatusCards } from './ServerStatusCards';

import type { ReactNode } from 'react';
import type { MonitoringRange } from '@/api/monitoring';
import type { MonitoringNode, MonitoringPoint, NodeEngine } from '@/api/types';

// recharts stays out of the shell bundle - only NodeSeriesChart.tsx imports it.
const NodeSeriesChart = lazy(() => import('./NodeSeriesChart').then((m) => ({ default: m.NodeSeriesChart })));

const VIEWS = ['overview', 'nodes', 'web'] as const;

const DEFAULT_BRAND_PRIMARY = '#0b7285';
const DEFAULT_BRAND_ACCENT = '#099268';

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
      <div className="flex min-h-16 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b border-hairline px-5 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <StatusBadge status={node.status} hideLabel />
          <h2 className="truncate text-title text-foreground">{node.node_name}</h2>
          <span className="mono truncate text-mono text-mute">{node.hostname}</span>
        </div>
        <Button
          variant="outline"
          size="sm"
          className="shrink-0"
          nativeButton={false}
          render={<Link to={`/nodes/${node.node_id}`} />}
        >
          {t('monitoring.open_node')}
        </Button>
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
      <div className="flex min-h-16 items-center gap-3 border-b border-hairline px-5 py-3">
        <Skeleton className="h-4 w-28" />
        <Skeleton className="h-3 w-40" />
      </div>
      <PanelBody>
        <ChartSkeleton />
      </PanelBody>
    </Panel>
  );
}

function MonitoringView() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { isWriter } = useAuth();
  const [range, setRange] = useState<MonitoringRange>('24h');
  const [view, setView] = useSection(VIEWS, 'overview');
  const overviewQuery = useMonitoringOverview(range);
  const nodesQuery = useNodes();
  const rollouts = useFleetRollouts();
  const { branding } = useBrandingIdentity();

  const servers = useMemo(() => nodesQuery.data?.items ?? [], [nodesQuery.data]);
  const engineById = new Map(servers.map((n) => [n.id, n.engine]));
  const series = overviewQuery.data?.series ?? {};
  const entries = inServerOrder(overviewQuery.data?.nodes ?? [], servers);
  const certs = new Map((overviewQuery.data?.nodes ?? []).map((n) => [n.node_id, n.cert_expires_at]));
  const loading = overviewQuery.isLoading || nodesQuery.isLoading;
  const failed = overviewQuery.isError || nodesQuery.isError;
  const error = overviewQuery.error ?? nodesQuery.error;

  const colors = useMemo((): [string, string, string] => {
    const [first, second, third] = seriesPalette(
      branding?.primary_color || DEFAULT_BRAND_PRIMARY,
      branding?.accent_color || DEFAULT_BRAND_ACCENT,
      3,
    );
    return [first, second, third];
  }, [branding?.primary_color, branding?.accent_color]);

  const body = () => {
    if (loading && view === 'overview') return <FleetSummary nodes={[]} loading />;
    if (loading) {
      return (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <NodeCardSkeleton />
          <NodeCardSkeleton />
        </div>
      );
    }
    if (failed) {
      return (
        <ErrorState
          message={error instanceof ApiError ? error.message : t('common.error_generic')}
          retryLabel={t('common.refresh')}
          onRetry={() => {
            void overviewQuery.refetch();
            void nodesQuery.refetch();
          }}
        />
      );
    }
    if (servers.length === 0) {
      return (
        <EmptyState
          icon={Server}
          title={t('monitoring.empty_no_nodes')}
          action={
            <Button type="button" onClick={() => navigate('/nodes')}>
              {t('nodes.add')}
            </Button>
          }
        />
      );
    }
    if (view === 'nodes') {
      return (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          {entries.map((node, i) => (
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
      );
    }
    if (view === 'web') return <FleetCarriersCard nodes={servers} />;
    return (
      <div className="space-y-6">
        <FleetSummary nodes={servers} overview={overviewQuery.data} />
        <section aria-labelledby="monitoring-servers" className="space-y-3">
          <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <h2 id="monitoring-servers" className="text-title text-foreground">
              {t('nodes.title')}
            </h2>
            {isWriter && servers.length > 1 && <p className="text-label text-mute">{t('nodes.reorder_hint_cards')}</p>}
          </div>
          <ServerStatusCards nodes={servers} certs={certs} pinned={rollouts.data?.version} writer={isWriter} />
        </section>
      </div>
    );
  };

  return (
    <>
      <PageHeader
        title={t('monitoring.title')}
        description={t('workspace.monitoring_hint')}
        actions={
          <>
            <HelpButton topic="monitoring" />
            {view === 'nodes' && (
              <SegmentedControl
                label={t('monitoring.range_label')}
                value={range}
                onChange={setRange}
                options={MONITORING_RANGES.map((r) => ({ value: r, label: t(`monitoring.range_${r}`) }))}
              />
            )}
          </>
        }
      />

      <SectionTabs
        label={t('monitoring.view_label')}
        value={view}
        onChange={setView}
        items={VIEWS.map((value) => ({ value, label: t(`monitoring.view_${value}`) }))}
      />

      {body()}
    </>
  );
}

/** Fleet health and per-server charts. The metrics export that used to live here is in Settings now. */
export function MonitoringPage() {
  const [params] = useSearchParams();
  if (params.get('section') === 'integrations' || params.get('view') === 'integrations') {
    return <Navigate to="/settings?section=integrations" replace />;
  }
  return <MonitoringView />;
}
