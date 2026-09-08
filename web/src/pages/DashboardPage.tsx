import {
  Activity,
  ArrowDown,
  ArrowUp,
  Bell,
  Clock,
  Gauge,
  KeyRound,
  Plus,
  Radio,
  Server,
  ServerOff,
  TriangleAlert,
  Waves,
} from 'lucide-react';
import { Suspense, lazy, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';

import { useBranding } from '@/api/branding';
import { useDashboardSummary, useNodesSeries24h } from '@/api/dashboard';
import { useNodes } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { ChartLegend } from '@/components/common/ChartLegend';
import { EmptyState } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { PageHeader } from '@/components/common/PageHeader';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { StatGrid } from '@/components/common/StatGrid';
import { statTone } from '@/components/common/statTone';
import { Button } from '@/components/ui/button';
import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { OFFLINE_SERIES_COLOR, seriesPalette } from '@/lib/chart';
import { formatCompactAge, formatCompactDuration, formatNumber, splitBytes } from '@/lib/format';
import { cn } from '@/lib/utils';
import { DASH, nodeLoad } from '@/pages/nodes/nodeDisplay';

import { AlertsSection } from './dashboard/AlertsSection';
import { NodesTable, NodesTableSkeleton } from './dashboard/NodesTable';
import { RecentJobsSection } from './dashboard/RecentJobsSection';

import type { StatGridTile } from '@/components/common/StatGrid';
import type { SessionsSeriesConfig } from './dashboard/SessionsChart';
import type { Node, SeriesPoint } from '@/api/types';

// recharts stays out of the shell bundle - only SessionsChart.tsx imports it.
const SessionsChart = lazy(() => import('./dashboard/SessionsChart').then((m) => ({ default: m.SessionsChart })));

// index.css ships these as the --brand-primary/--brand-accent defaults; the
// palette needs the literal values, not the CSS variables, to compare hues.
const DEFAULT_BRAND_PRIMARY = '#3b82f6';
const DEFAULT_BRAND_ACCENT = '#22c55e';

const RECENT_JOBS_LIMIT = 6;
const DELTA_WINDOW_MS = 60 * 60 * 1000;
// A point more than 15 min away from "an hour ago" is not an hour-ago reading.
const DELTA_TOLERANCE_MS = 15 * 60 * 1000;
/** Offline nodes listed by name in the tile context; past this, just the count. */
const MAX_NAMED_OFFLINE = 2;
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

/**
 * Percentage change in total live sessions over the last hour, or null when
 * the series does not reach back that far. Reported as a whole percent - the
 * number is a direction of travel, and a decimal would imply a precision the
 * sampling does not have.
 */
function sessionsHourChange(seriesByNode: Record<string, SeriesPoint[]>): number | null {
  const totals = new Map<string, number>();
  for (const points of Object.values(seriesByNode)) {
    for (const p of points) totals.set(p.t, (totals.get(p.t) ?? 0) + p.sessions_live);
  }
  const rows = [...totals.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  if (rows.length < 2) return null;

  const lastTime = new Date(rows[rows.length - 1][0]).getTime();
  if (Number.isNaN(lastTime)) return null;
  const target = lastTime - DELTA_WINDOW_MS;

  let closest: [string, number] | null = null;
  let closestDiff = Number.POSITIVE_INFINITY;
  for (const row of rows) {
    const diff = Math.abs(new Date(row[0]).getTime() - target);
    if (diff < closestDiff) {
      closestDiff = diff;
      closest = row;
    }
  }
  if (!closest || closestDiff > DELTA_TOLERANCE_MS || closest[1] <= 0) return null;

  return Math.round(((rows[rows.length - 1][1] - closest[1]) / closest[1]) * 100);
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
    // A node that stopped reporting is not one more category on the chart -
    // it is an absence, so it goes grey and lets the live nodes keep the hues.
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

/**
 * "обновлено 12 с назад" beside the page title. It ticks in its own component
 * so the second hand never re-renders the chart underneath it.
 */
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
  const { data: branding } = useBranding();
  const nodes = useMemo(() => nodesQuery.data?.items ?? [], [nodesQuery.data]);
  const nodeIds = useMemo(() => nodes.map((n) => n.id), [nodes]);
  const seriesResults = useNodesSeries24h(nodeIds);

  const summary = summaryQuery.data;
  const nodesOnline = summary?.nodes.online ?? 0;
  const nodesTotal = summary?.nodes.total ?? 0;
  const nodesOffline = summary?.nodes.offline ?? 0;
  const nodesDegraded = summary?.nodes.degraded ?? 0;
  const keysActive = summary?.keys.active ?? 0;
  const keysPending = summary?.keys.pending ?? 0;
  const keysTotal = summary?.keys.total ?? 0;

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

  const traffic = useMemo(() => {
    let up = 0;
    let down = 0;
    for (const points of Object.values(seriesByNode)) {
      const d = trafficDeltas(points);
      up += d.up;
      down += d.down;
    }
    return { up, down };
  }, [seriesByNode]);

  const chart = useMemo(
    () => buildChartData(nodes, seriesByNode, palette, t('dashboard.sessions_chart_other')),
    [nodes, seriesByNode, palette, t],
  );

  // Sessions per node for the table: the freshest sample, but only from a node
  // that is still reporting - a three-hour-old count is not a live count.
  const sessionsByNode = useMemo(() => {
    const out: Record<string, number | undefined> = {};
    for (const n of nodes) {
      out[n.id] = n.status === 'offline' ? undefined : lastPoint(seriesByNode[n.id] ?? [])?.sessions_live;
    }
    return out;
  }, [nodes, seriesByNode]);

  const sessionsChange = useMemo(() => sessionsHourChange(seriesByNode), [seriesByNode]);

  /*
   * The hour's movement, as one mono line. It is not tinted: inside a tile
   * the only colour is the icon's, and the arrow already says which way the
   * number went - a green "up" would also be wrong half the time, since more
   * sessions is not automatically good news.
   */
  const sessionsDelta = useMemo((): string | undefined => {
    if (sessionsChange === null) return undefined;
    const arrow = sessionsChange > 0 ? '▲' : sessionsChange < 0 ? '▼' : '·';
    return t('dashboard.delta_per_hour', { value: `${arrow} ${Math.abs(sessionsChange)}%` });
  }, [sessionsChange, t]);

  const offlineContext = useMemo(() => {
    const offline = nodes.filter((n) => n.status === 'offline');
    if (offline.length === 0) return undefined;
    if (offline.length > MAX_NAMED_OFFLINE) return t('dashboard.offline_many', { count: offline.length });
    if (offline.length === 1) {
      const age = formatCompactAge(offline[0].last_seen_at, i18n.language);
      return age
        ? t('dashboard.offline_one', { name: offline[0].name, ago: age })
        : t('dashboard.offline_names', { names: offline[0].name });
    }
    return t('dashboard.offline_names', { names: offline.map((n) => n.name).join(', ') });
  }, [nodes, t, i18n.language]);

  const chartStep = useMemo(() => {
    if (chart.data.length < 2) return null;
    const ms = new Date(String(chart.data[1].t)).getTime() - new Date(String(chart.data[0].t)).getTime();
    return Number.isFinite(ms) && ms > 0 ? formatCompactDuration(ms / 1000, i18n.language) : null;
  }, [chart.data, i18n.language]);

  /**
   * The colour each node already has in the chart above the table, so the
   * sparkline in a row and the line in the chart are the same node. Nodes
   * past the palette are folded into one "other" series there and have no
   * colour of their own; the table falls back to --series-other for them.
   */
  const colorByNode = useMemo(() => {
    const out: Record<string, string> = {};
    for (const s of chart.series) if (s.key !== OTHER_SERIES_KEY) out[s.key] = s.color;
    return out;
  }, [chart.series]);

  /**
   * Average CPU across the nodes that are actually reporting. `nodeLoad`
   * returns nothing for an offline node or one that never reported, and an
   * average that counted those as zero would say the fleet is idle when half
   * of it is unreachable. Null when nothing is reporting at all.
   */
  const avgLoad = useMemo(() => {
    const cpus = nodes.map((n) => nodeLoad(n)?.cpu).filter((v): v is number => v !== undefined);
    if (cpus.length === 0) return null;
    return cpus.reduce((sum, v) => sum + v, 0) / cpus.length;
  }, [nodes]);

  const recentJobs = (summary?.recent_jobs ?? []).slice(0, RECENT_JOBS_LIMIT);
  // Memoised rather than derived inline: the tile list below depends on it,
  // and a fresh [] on every render would rebuild eleven tiles for nothing.
  const openAlerts = useMemo(() => summary?.alerts ?? [], [summary?.alerts]);
  const trafficUp = splitBytes(traffic.up);
  const trafficDown = splitBytes(traffic.down);
  const loading = summaryQuery.isLoading || nodesQuery.isLoading;
  const failed = summaryQuery.isError || nodesQuery.isError;

  /*
   * The eleven facts, in the order the mockup reads them: the fleet, then
   * what is on it, then what is moving through it, then what wants
   * attention. Each tile's tone is decided by `statTone` from the fact
   * itself, never set here - a tile is neutral until its number means
   * something is wrong, which is what makes the one coloured tile on a
   * healthy page worth looking at.
   */
  const tiles = useMemo((): StatGridTile[] => {
    const num = (v: number) => formatNumber(v, i18n.language);
    return [
      {
        id: 'nodes-online',
        icon: Server,
        tone: statTone({ kind: 'nodes_online', online: nodesOnline, total: nodesTotal }),
        label: t('dashboard.nodes_online'),
        value: num(nodesOnline),
        unit: `/ ${num(nodesTotal)}`,
        context: t('dashboard.tile_nodes_online_context'),
        to: '/nodes',
        loading,
      },
      {
        id: 'nodes-offline',
        icon: ServerOff,
        tone: statTone({ kind: 'nodes_offline', count: nodesOffline }),
        label: t('dashboard.tile_nodes_offline'),
        value: num(nodesOffline),
        context: offlineContext ?? t('dashboard.tile_nodes_offline_none'),
        to: '/nodes',
        loading,
      },
      {
        id: 'nodes-degraded',
        icon: TriangleAlert,
        tone: statTone({ kind: 'degraded', count: nodesDegraded }),
        label: t('dashboard.tile_degraded'),
        value: num(nodesDegraded),
        context: nodesDegraded > 0 ? t('dashboard.tile_degraded_some') : t('dashboard.tile_degraded_none'),
        to: '/nodes',
        loading,
      },
      {
        id: 'keys-active',
        icon: KeyRound,
        tone: statTone({ kind: 'stateless' }),
        label: t('dashboard.keys_active'),
        value: num(keysActive),
        context: t('dashboard.tile_keys_total', { count: keysTotal }),
        to: '/keys',
        loading,
      },
      {
        id: 'keys-pending',
        icon: Clock,
        tone: statTone({ kind: 'keys_pending', count: keysPending }),
        label: t('dashboard.tile_keys_pending'),
        value: num(keysPending),
        context: keysPending > 0 ? t('dashboard.tile_keys_pending_queued') : t('dashboard.tile_keys_pending_none'),
        to: '/keys',
        loading,
      },
      {
        id: 'sessions',
        icon: Radio,
        tone: statTone({ kind: 'stateless' }),
        label: t('dashboard.sessions_live'),
        value: num(summary?.sessions_live ?? 0),
        delta: sessionsDelta,
        context: sessionsDelta ? undefined : t('dashboard.tile_sessions_context'),
        to: '/monitoring',
        loading,
      },
      {
        id: 'streams',
        icon: Waves,
        tone: statTone({ kind: 'stateless' }),
        label: t('dashboard.tile_streams'),
        value: num(summary?.streams_live ?? 0),
        context: t('dashboard.tile_streams_context'),
        to: '/monitoring',
        loading,
      },
      {
        id: 'traffic-up',
        icon: ArrowUp,
        tone: statTone({ kind: 'stateless' }),
        label: t('dashboard.tile_traffic_up'),
        value: trafficUp.value,
        unit: trafficUp.unit,
        context: t('dashboard.tile_traffic_context'),
        to: '/monitoring',
        loading: loading || seriesLoading,
      },
      {
        id: 'traffic-down',
        icon: ArrowDown,
        tone: statTone({ kind: 'stateless' }),
        label: t('dashboard.tile_traffic_down'),
        value: trafficDown.value,
        unit: trafficDown.unit,
        context: t('dashboard.tile_traffic_context'),
        to: '/monitoring',
        loading: loading || seriesLoading,
      },
      {
        id: 'alerts-open',
        icon: Bell,
        tone: statTone({ kind: 'alerts_open', count: openAlerts.length }),
        label: t('dashboard.tile_alerts_open'),
        value: num(openAlerts.length),
        // The newest alert names itself in the machine's own words, which is
        // what an operator matches against the list two panels down.
        context:
          openAlerts.length > 0
            ? [openAlerts[0].kind, openAlerts[0].node_name].filter(Boolean).join(' · ')
            : t('dashboard.tile_alerts_none'),
        loading,
      },
      {
        id: 'avg-load',
        icon: Gauge,
        tone: statTone({ kind: 'avg_load', percent: avgLoad }),
        label: t('dashboard.tile_avg_load'),
        value: avgLoad === null ? DASH : num(Math.round(avgLoad)),
        unit: avgLoad === null ? undefined : '%',
        context: t('dashboard.tile_avg_load_context'),
        to: '/monitoring',
        loading,
      },
    ];
  }, [
    avgLoad,
    i18n.language,
    keysActive,
    keysPending,
    keysTotal,
    loading,
    nodesDegraded,
    nodesOffline,
    nodesOnline,
    nodesTotal,
    offlineContext,
    openAlerts,
    seriesLoading,
    sessionsDelta,
    summary?.sessions_live,
    summary?.streams_live,
    t,
    trafficDown.unit,
    trafficDown.value,
    trafficUp.unit,
    trafficUp.value,
  ]);

  /*
   * The error branch comes before the empty one on purpose. When /nodes fails
   * the list falls back to [], and without this the operator whose API is down
   * is told "no nodes yet, add one" about a fleet that is running.
   */
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
        actions={<HelpButton topic="dashboard" />}
      />

      {/* The three blocks arrive in the order they are read: the numbers, the
          shape of the last day, then the fleet itself. The grid carries no
          entrance of its own - each tile brings its own staggered one, and
          the loading state is the same eleven tiles with skeletons in them,
          so the row does not change shape when the data lands. */}
      <StatGrid tiles={tiles} />

      <div className={cn(ENTER_CLASS, 'grid grid-cols-1 gap-4 lg:grid-cols-12')} style={enterDelay(1)}>
        <Panel className="flex flex-col lg:col-span-8">
          {/* No range control and no expand button here, unlike the mockup:
              the series behind this chart is a fixed 24h fetch and there is
              no full-screen view to open, so either affordance would be a
              control that does nothing. */}
          <PanelHeader
            icon={Activity}
            title={t('dashboard.sessions_chart_title')}
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

        <Panel className="lg:col-span-4">
          <AlertsSection />
          <RecentJobsSection jobs={recentJobs} />
        </Panel>
      </div>

      {/* Wrapped rather than classed directly: Panel takes a className but no
          style, and the entrance needs its index on the element. */}
      <div className={ENTER_CLASS} style={enterDelay(2)}>
        <Panel>
          <PanelHeader
            icon={Server}
            title={t('nodes.title')}
            meta={loading ? undefined : String(nodes.length)}
            actions={
              isWriter && (
                <Button type="button" variant="outline" size="sm" render={<Link to="/nodes" />}>
                  <Plus />
                  {t('nodes.add')}
                </Button>
              )
            }
          />
          {loading ? (
            <NodesTableSkeleton />
          ) : (
            <NodesTable
              nodes={nodes}
              sessionsByNode={sessionsByNode}
              seriesByNode={seriesByNode}
              colorByNode={colorByNode}
            />
          )}
        </Panel>
      </div>
    </>
  );
}
