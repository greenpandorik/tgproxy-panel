import { Suspense, lazy, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';

import { useBranding } from '@/api/branding';
import { useDashboardSummary, useNodesSeries24h } from '@/api/dashboard';
import { useNodes } from '@/api/nodes';
import { ChartLegend } from '@/components/common/ChartLegend';
import { EmptyState } from '@/components/common/EmptyState';
import { PageHeader } from '@/components/common/PageHeader';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { StatCard } from '@/components/common/StatCard';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { OFFLINE_SERIES_COLOR, seriesPalette } from '@/lib/chart';
import { formatCompactAge, formatCompactDuration, formatBytes, formatNumber, splitBytes } from '@/lib/format';

import { AlertsSection } from './dashboard/AlertsSection';
import { NodesTable } from './dashboard/NodesTable';
import { RecentJobsSection } from './dashboard/RecentJobsSection';

import type { DeltaTone } from '@/components/common/StatCard';
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
    series.push({ key: '__other', name: otherLabel, color: 'var(--series-other)' });
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
      row.__other = (Number(row.__other) || 0) + p.sessions_live;
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
  const keysActive = summary?.keys.active ?? 0;
  const keysPending = summary?.keys.pending ?? 0;

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

  const sessionsDelta = useMemo((): { text: string; tone: DeltaTone } | undefined => {
    if (sessionsChange === null) return undefined;
    const arrow = sessionsChange > 0 ? '▲' : sessionsChange < 0 ? '▼' : '·';
    const tone: DeltaTone = sessionsChange > 0 ? 'ok' : sessionsChange < 0 ? 'err' : 'neutral';
    return { text: t('dashboard.delta_per_hour', { value: `${arrow} ${Math.abs(sessionsChange)}%` }), tone };
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

  const recentJobs = (summary?.recent_jobs ?? []).slice(0, RECENT_JOBS_LIMIT);
  const trafficTotal = splitBytes(traffic.up + traffic.down);
  const loading = summaryQuery.isLoading || nodesQuery.isLoading;

  if (!loading && nodes.length === 0) {
    return (
      <>
        <PageHeader title={t('dashboard.title')} />
        <EmptyState
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
      <PageHeader title={t('dashboard.title')} description={<UpdatedAgo at={summaryQuery.dataUpdatedAt} />} />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label={t('dashboard.nodes_online')}
          value={formatNumber(nodesOnline, i18n.language)}
          unit={`/ ${formatNumber(nodesTotal, i18n.language)}`}
          badge={nodesOffline > 0 ? t('dashboard.nodes_offline_suffix', { count: nodesOffline }) : undefined}
          context={offlineContext}
          loading={loading}
        />
        <StatCard
          label={t('dashboard.keys_active')}
          value={formatNumber(keysActive, i18n.language)}
          context={keysPending > 0 ? t('dashboard.keys_pending_context', { count: keysPending }) : undefined}
          loading={loading}
        />
        <StatCard
          label={t('dashboard.sessions_live')}
          value={formatNumber(summary?.sessions_live ?? 0, i18n.language)}
          delta={sessionsDelta}
          loading={loading}
        />
        <StatCard
          label={t('dashboard.traffic_title')}
          value={trafficTotal.value}
          unit={trafficTotal.unit}
          context={`↑ ${formatBytes(traffic.up)} · ↓ ${formatBytes(traffic.down)}`}
          loading={loading || seriesLoading}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-12">
        <Panel className="flex flex-col lg:col-span-8">
          <PanelHeader
            title={t('dashboard.sessions_chart_title')}
            meta={chartStep ? t('dashboard.sessions_chart_meta', { step: chartStep }) : undefined}
          />
          <div className="flex-1 px-2 pt-3">
            {seriesLoading ? (
              <Skeleton className="h-[220px] w-full" />
            ) : chart.data.length === 0 ? (
              <div className="flex h-[220px] items-center justify-center">
                <p className="text-sm text-mute">{t('dashboard.sessions_chart_empty')}</p>
              </div>
            ) : (
              <Suspense fallback={<Skeleton className="h-[220px] w-full" />}>
                <SessionsChart data={chart.data} series={chart.series} />
              </Suspense>
            )}
          </div>
          <ChartLegend items={chart.series} className="border-t border-hairline px-4 py-2.5" />
        </Panel>

        <Panel className="lg:col-span-4">
          <AlertsSection />
          <RecentJobsSection jobs={recentJobs} />
        </Panel>
      </div>

      <Panel>
        <PanelHeader title={t('nodes.title')} meta={String(nodes.length)} />
        <NodesTable nodes={nodes} sessionsByNode={sessionsByNode} />
      </Panel>
    </>
  );
}
