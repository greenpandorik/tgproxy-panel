import { Activity } from 'lucide-react';
import { Suspense, lazy, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { useNodesSeries24h } from '@/api/dashboard';
import { AdvancedSettings } from '@/components/common/AdvancedSettings';
import { ChartLegend } from '@/components/common/ChartLegend';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { OFFLINE_SERIES_COLOR, seriesPalette } from '@/lib/chart';
import { formatCompactDuration } from '@/lib/format';
import { peopleOrConnections } from '@/pages/nodes/nodeDisplay';
import { useBrandingIdentity } from '@/theme/ThemeProvider';

import { RecentJobsSection } from './RecentJobsSection';

import type { SessionsSeriesConfig } from './SessionsChart';
import type { ApplyJob, Node, SeriesPoint } from '@/api/types';

const SessionsChart = lazy(() => import('./SessionsChart').then((m) => ({ default: m.SessionsChart })));

const DEFAULT_BRAND_PRIMARY = '#0b7285';
const DEFAULT_BRAND_ACCENT = '#099268';

/** The chart's bucket for every server past the palette. Not a server id. */
const OTHER_SERIES_KEY = '__other';

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
    color: n.status === 'offline' ? OFFLINE_SERIES_COLOR : palette[i],
  }));
  if (rest.length > 0) {
    series.push({ key: OTHER_SERIES_KEY, name: otherLabel, color: 'var(--series-other)' });
  }

  const rows = new Map<string, Record<string, number | string>>();
  for (const n of main) {
    for (const p of seriesByNode[n.id] ?? []) {
      const row = rows.get(p.t) ?? { t: p.t };
      row[n.id] = peopleOrConnections(p);
      rows.set(p.t, row);
    }
  }
  for (const n of rest) {
    for (const p of seriesByNode[n.id] ?? []) {
      const row = rows.get(p.t) ?? { t: p.t };
      row[OTHER_SERIES_KEY] = (Number(row[OTHER_SERIES_KEY]) || 0) + peopleOrConnections(p);
      rows.set(p.t, row);
    }
  }

  const data = [...rows.values()].sort((a, b) => String(a.t).localeCompare(String(b.t)));
  return { data, series };
}

/** The collapsed 24-hour people chart and the last operations, under the cards. */
export function HistorySection({ nodes, jobs }: { nodes: Node[]; jobs: ApplyJob[] }) {
  const { t, i18n } = useTranslation();
  const { branding } = useBrandingIdentity();
  const nodeIds = useMemo(() => nodes.map((n) => n.id), [nodes]);
  const seriesResults = useNodesSeries24h(nodeIds);

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
  const chart = useMemo(
    () => buildChartData(nodes, seriesByNode, palette, t('dashboard.sessions_chart_other')),
    [nodes, seriesByNode, palette, t],
  );
  const chartStep = useMemo(() => {
    if (chart.data.length < 2) return null;
    const ms = new Date(String(chart.data[1].t)).getTime() - new Date(String(chart.data[0].t)).getTime();
    return Number.isFinite(ms) && ms > 0 ? formatCompactDuration(ms / 1000, i18n.language) : null;
  }, [chart.data, i18n.language]);

  return (
    <AdvancedSettings label={t('workspace.history_details')}>
      <div className={ENTER_CLASS} style={enterDelay(2)}>
        <Panel className="flex flex-col">
          <PanelHeader
            icon={Activity}
            title={t('dashboard.sessions_chart_title')}
            actions={
              <Button variant="outline" size="sm" nativeButton={false} render={<Link to="/monitoring" />}>
                {t('nav.monitoring')}
              </Button>
            }
            meta={chartStep ? t('dashboard.sessions_chart_meta', { step: chartStep }) : undefined}
          />
          <div className="flex-1 px-2 py-3">
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
          {!seriesLoading && chart.data.length > 0 && (
            <ChartLegend items={chart.series} className="border-t border-hairline px-4 py-3" />
          )}
        </Panel>
      </div>

      <div className={ENTER_CLASS} style={enterDelay(3)}>
        <Panel>
          <RecentJobsSection jobs={jobs} />
        </Panel>
      </div>
    </AdvancedSettings>
  );
}
