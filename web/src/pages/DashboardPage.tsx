import { Activity, KeyRound, Server } from 'lucide-react';
import { Suspense, lazy, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';

import { useBranding } from '@/api/branding';
import { useAlerts, useDashboardSummary, useNodesSeries24h } from '@/api/dashboard';
import { useNodes } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { ChartLegend } from '@/components/common/ChartLegend';
import { EmptyState } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { PageHeader } from '@/components/common/PageHeader';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { OFFLINE_SERIES_COLOR, seriesPalette } from '@/lib/chart';
import { formatCompactAge, formatCompactDuration } from '@/lib/format';

import { AttentionSection } from './dashboard/AttentionSection';
import { DashboardMetrics } from './dashboard/DashboardMetrics';
import { NodesTable, NodesTableSkeleton } from './dashboard/NodesTable';
import { RecentJobsSection } from './dashboard/RecentJobsSection';
import { VerdictLine } from './dashboard/VerdictLine';

import type { SessionsSeriesConfig } from './dashboard/SessionsChart';
import type { Node, SeriesPoint } from '@/api/types';

// recharts stays out of the shell bundle - only SessionsChart.tsx imports it.
const SessionsChart = lazy(() => import('./dashboard/SessionsChart').then((m) => ({ default: m.SessionsChart })));

const DEFAULT_BRAND_PRIMARY = '#3b82f6';
const DEFAULT_BRAND_ACCENT = '#22c55e';

const RECENT_JOBS_LIMIT = 6;

/** The chart's bucket for every node past the palette. Not a node id. */
const OTHER_SERIES_KEY = '__other';

function lastPoint(points: SeriesPoint[]): SeriesPoint | undefined {
  return points.length > 0 ? points[points.length - 1] : undefined;
}

/** Total bytes moved across the whole window, as the difference between its ends. */
function trafficDeltas(points: SeriesPoint[]): { up: number; down: number } {
  if (points.length < 2) return { up: 0, down: 0 };
  const first = points[0];
  const last = points[points.length - 1];
  return {
    up: Math.max(0, last.bytes_up - first.bytes_up),
    down: Math.max(0, last.bytes_down - first.bytes_down),
  };
}

function buildChartData(
  nodes: Node[],
  seriesByNode: Record<string, SeriesPoint[]>,
  palette: string[],
  otherLabel: string,
): { data: Array<Record<string, number | string>>; series: SessionsSeriesConfig[] } {
  const main = nodes.slice(0, palette.length);
  const rest = nodes.slice(palette.length);

  const series: SessionsSeriesConfig[] = main.map((n, i) => ({
    key: n.id,
    name: n.name,
    // A node that stopped reporting is not one more category on the chart.
    color: n.status === 'offline' ? OFFLINE_SERIES_COLOR : palette[i],
  }));
  if (rest.length > 0) {
    series.push({ key: OTHER_SERIES_KEY, name: otherLabel, color: 'var(--series-other)' });
  }

  const rows = new Map<string, Record<string, number | string>>();
  for (const n of main) {
    for (const p of seriesByNode[n.id] ?? []) {
      const row = rows.get(p.t) ?? { t: p.t };
      row[n.id] = p.sessions_live;
      rows.set(p.t, row);
    }
  }
  for (const n of rest) {
    for (const p of seriesByNode[n.id] ?? []) {
      const row = rows.get(p.t) ?? { t: p.t };
      row[OTHER_SERIES_KEY] = (Number(row[OTHER_SERIES_KEY]) || 0) + p.sessions_live;
      rows.set(p.t, row);
    }
  }

  const data = [...rows.values()].sort((a, b) => String(a.t).localeCompare(String(b.t)));
  return { data, series };
}

function UpdatedAgo({ at }: { at: number }) {
  const { t, i18n } = useTranslation();
  const [, setTick] = useState(0);

  useEffect(() => {
    const id = window.setInterval(() => setTick((n) => n + 1), 1000);
    return () => window.clearInterval(id);
  }, []);

  if (!at) return null;
  const age = formatCompactAge(at, i18n.language);
  return age ? <>{t('dashboard.updated_ago', { value: age })}</> : null;
}

export function DashboardPage() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();

  const { isWriter } = useAuth();

  const summaryQuery = useDashboardSummary();
  const nodesQuery = useNodes();
  const alertsQuery = useAlerts();
  const { data: branding } = useBranding();
  const nodes = useMemo(() => nodesQuery.data?.items ?? [], [nodesQuery.data]);
  const nodeIds = useMemo(() => nodes.map((n) => n.id), [nodes]);
  const seriesResults = useNodesSeries24h(nodeIds);

  const summary = summaryQuery.data;

  const seriesByNode = useMemo(() => {
    const out: Record<string, SeriesPoint[]> = {};
    nodes.forEach((n, i) => {
      out[n.id] = seriesResults[i]?.data?.points ?? [];
    });
    return out;
  }, [nodes, seriesResults]);

  const seriesLoading = nodes.length > 0 && seriesResults.some((r) => r.isLoading);

  const palette = useMemo(
    () => seriesPalette(branding?.primary_color || DEFAULT_BRAND_PRIMARY, branding?.accent_color || DEFAULT_BRAND_ACCENT, 6),
    [branding?.primary_color, branding?.accent_color],
  );

  // Absent, not zero, while no node has two samples to subtract.
  const traffic = useMemo((): number | undefined => {
    let total: number | undefined;
    for (const points of Object.values(seriesByNode)) {
      if (points.length < 2) continue;
      const d = trafficDeltas(points);
      total = (total ?? 0) + d.up + d.down;
    }
    return total;
  }, [seriesByNode]);

  const chart = useMemo(
    () => buildChartData(nodes, seriesByNode, palette, t('dashboard.sessions_chart_other')),
    [nodes, seriesByNode, palette, t],
  );

  // Sessions per node for the table: the freshest sample, but only from a node that is still reporting.
  const sessionsByNode = useMemo(() => {
    const out: Record<string, number | undefined> = {};
    for (const n of nodes) {
      out[n.id] = n.status === 'offline' ? undefined : lastPoint(seriesByNode[n.id] ?? [])?.sessions_live;
    }
    return out;
  }, [nodes, seriesByNode]);

  const chartStep = useMemo(() => {
    if (chart.data.length < 2) return null;
    const ms = new Date(String(chart.data[1].t)).getTime() - new Date(String(chart.data[0].t)).getTime();
    return Number.isFinite(ms) && ms > 0 ? formatCompactDuration(ms / 1000, i18n.language) : null;
  }, [chart.data, i18n.language]);

  const colorByNode = useMemo(() => {
    const out: Record<string, string> = {};
    for (const s of chart.series) if (s.key !== OTHER_SERIES_KEY) out[s.key] = s.color;
    return out;
  }, [chart.series]);

  const recentJobs = (summary?.recent_jobs ?? []).slice(0, RECENT_JOBS_LIMIT);
  const loading = summaryQuery.isLoading || nodesQuery.isLoading;
  const failed = summaryQuery.isError || nodesQuery.isError;

  // The error branch comes before the empty one on purpose.
  if (!loading && failed) {
    return (
      <>
        <PageHeader title={t('dashboard.title')} actions={<HelpButton topic="dashboard" />} />
        <ErrorState
          message={
            summaryQuery.error instanceof ApiError
              ? summaryQuery.error.message
              : nodesQuery.error instanceof ApiError
                ? nodesQuery.error.message
                : t('common.error_generic')
          }
          retryLabel={t('common.refresh')}
          onRetry={() => {
            void summaryQuery.refetch();
            void nodesQuery.refetch();
          }}
        />
      </>
    );
  }

  if (!loading && nodes.length === 0) {
    return (
      <>
        <PageHeader title={t('dashboard.title')} actions={<HelpButton topic="dashboard" />} />
        <EmptyState
          icon={Server}
          title={t('dashboard.empty_no_nodes')}
          action={
            <Button type="button" onClick={() => navigate('/nodes')}>
              {t('nodes.add')}
            </Button>
          }
        />
      </>
    );
  }

  return (
    <>
      <PageHeader
        title={t('dashboard.title')}
        description={<UpdatedAgo at={summaryQuery.dataUpdatedAt} />}
        actions={
          <>
            {isWriter && (
              <Button nativeButton={false} render={<Link to="/keys" />}>
                <KeyRound />
                {t('dashboard.manage_access')}
              </Button>
            )}
            <HelpButton topic="dashboard" />
          </>
        }
      />

      <VerdictLine
        loading={loading}
        facts={{
          online: summary?.nodes.online,
          total: summary?.nodes.total,
          offline: summary?.nodes.offline,
          degraded: summary?.nodes.degraded,
          attention: alertsQuery.isError ? undefined : alertsQuery.data?.items.length,
        }}
      />

      <DashboardMetrics
        loading={loading}
        nodesOnline={summary?.nodes.online}
        nodesTotal={summary?.nodes.total}
        keysActive={summary?.keys.active}
        sessions={summary?.sessions_live}
        traffic={seriesLoading ? undefined : traffic}
      />

      <AttentionSection />

      {/* Wrapped rather than classed directly: Panel takes a className but no
          style, and the entrance needs its index on the element. */}
      <div className={ENTER_CLASS} style={enterDelay(1)}>
        <Panel>
          <PanelHeader
            icon={Server}
            title={t('nodes.title')}
            meta={loading ? undefined : String(nodes.length)}
            actions={
              <Button type="button" variant="outline" size="sm" nativeButton={false} render={<Link to="/nodes" />}>
                {t('dashboard.all_servers')}
              </Button>
            }
          />
          {loading ? (
            <NodesTableSkeleton />
          ) : (
            <NodesTable nodes={nodes} sessionsByNode={sessionsByNode} seriesByNode={seriesByNode} colorByNode={colorByNode} />
          )}
        </Panel>
      </div>

      <div className={ENTER_CLASS} style={enterDelay(2)}>
        <Panel className="flex flex-col">
          {/* No range control and no expand button here, unlike the mockup:
              the series behind this chart is a fixed 24h fetch and there is
              no full-screen view to open, so either affordance would be a
              control that does nothing. */}
          <PanelHeader
            icon={Activity}
            title={t('dashboard.sessions_chart_title')}
            actions={
              <Button variant="ghost" size="sm" nativeButton={false} render={<Link to="/monitoring" />}>
                {t('nav.monitoring')}
              </Button>
            }
            meta={chartStep ? t('dashboard.sessions_chart_meta', { step: chartStep }) : undefined}
          />
          <div className="flex-1 px-2 pt-3">
            {seriesLoading ? (
              <Skeleton className="h-[220px] w-full" />
            ) : chart.data.length === 0 ? (
              <div className="flex h-[220px] items-center justify-center">
                <p className="text-body text-mute">{t('dashboard.sessions_chart_empty')}</p>
              </div>
            ) : (
              <Suspense fallback={<Skeleton className="h-[220px] w-full" />}>
                <SessionsChart data={chart.data} series={chart.series} />
              </Suspense>
            )}
          </div>
          <ChartLegend items={chart.series} className="border-t border-hairline px-4 py-3" />
        </Panel>
      </div>

      <div className={ENTER_CLASS} style={enterDelay(3)}>
        <Panel>
          <RecentJobsSection jobs={recentJobs} />
        </Panel>
      </div>
    </>
  );
}
