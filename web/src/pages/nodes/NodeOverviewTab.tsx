import { RefreshCw } from 'lucide-react';
import { Suspense, lazy, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useAlerts } from '@/api/dashboard';
import { useNodeSeries } from '@/api/monitoring';
import { useNodeHealth, useRunNodeCheck } from '@/api/nodes';
import { useNodeDiagnostics, useNodeWebCarriers, useRunWebDiagnostics } from '@/api/web';
import { useAuth } from '@/auth/AuthProvider';
import { CollapsibleSection } from '@/components/common/CollapsibleSection';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel } from '@/components/common/Panel';
import { SegmentedControl } from '@/components/common/SegmentedControl';
import { SubSection, SubSections } from '@/components/common/SubSection';
import { CAP_WEB, nodeCapability } from '@/components/web/capability';
import { WebDiagnosticsCard } from '@/components/web/WebDiagnosticsCard';
import { Button } from '@/components/ui/button';
import { ENTER_CLASS } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';
import { cn } from '@/lib/utils';

import { dcLatencyRows } from './dcDisplay';
import { loadRows } from './loadSeries';
import { NodeCheckCard } from './NodeCheckCard';
import { NodeDcsCard } from './NodeDcsCard';
import { NodeHealthStrip } from './NodeHealthStrip';
import { NodeProbes } from './NodeProbes';
import { ReliabilityRoutes } from './NodeReliability';
import { NodeServicesSection } from './NodeServicesSection';
import { NodeVerdict } from './NodeVerdict';
import { NodeWebSection } from './NodeWebSection';
import { checkTally, dcState, serviceReadings } from './nodeHealth';
import { freshProbeChecks, useNodeProbes } from './probes';
import { checksSummary, dcSummary, servicesSummary, webSummary } from './sectionSummaries';
import { nodeVerdict } from './serverVerdict';

import type { LiveState, Summary } from './sectionSummaries';
import type { MonitoringRange } from '@/api/monitoring';
import type { Node, SeriesPoint } from '@/api/types';
import type { ReactNode } from 'react';

const NodeLoadCharts = lazy(() => import('./NodeLoadCharts').then((m) => ({ default: m.NodeLoadCharts })));
const DcLatencyChart = lazy(() => import('./DcLatencyChart').then((m) => ({ default: m.DcLatencyChart })));

const RANGES: MonitoringRange[] = ['24h', '7d'];

function Section({
  summary,
  focus,
  ...props
}: {
  summary: Summary;
  focus?: boolean;
  title: string;
  id: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return <CollapsibleSection {...props} summary={summary.text} tone={summary.tone} defaultOpen={focus} />;
}

function LoadBlock({
  node,
  range,
  onRange,
  points,
  loading,
  error,
  onRetry,
}: {
  node: Node;
  range: MonitoringRange;
  onRange: (range: MonitoringRange) => void;
  points: SeriesPoint[];
  loading: boolean;
  error: boolean;
  onRetry: () => void;
}) {
  const { t } = useTranslation();
  const rows = useMemo(() => loadRows(points, node.engine), [points, node.engine]);
  return (
    <section aria-labelledby={`load-${node.id}`}>
      <div className="flex min-h-13 flex-wrap items-center justify-between gap-x-4 gap-y-2 px-(--panel-x) py-3">
        <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-0.5">
          <h2 id={`load-${node.id}`} className="text-body font-semibold text-foreground">
            {t('nodes.detail_load_title', { range: t(`monitoring.range_${range}`) })}
          </h2>
          <span className="text-label text-mute">
            {t(node.engine === 'telemt' ? 'nodes.detail_load_series' : 'nodes.detail_load_series_tproxy')}
          </span>
        </div>
        <SegmentedControl
          label={t('monitoring.range_label')}
          value={range}
          onChange={onRange}
          options={RANGES.map((r) => ({ value: r, label: t(`monitoring.range_${r}`) }))}
        />
      </div>
      <div className="px-(--panel-x) pb-5">
        {error ? (
          <ErrorState inset message={t('common.error_generic')} retryLabel={t('common.refresh')} onRetry={onRetry} />
        ) : loading ? (
          <Skeleton className="h-[148px] w-full" />
        ) : rows.length === 0 ? (
          <p className="py-6 text-center text-body text-mute">{t('nodes.load_empty')}</p>
        ) : (
          <Suspense fallback={<Skeleton className="h-[148px] w-full" />}>
            <NodeLoadCharts rows={rows} engine={node.engine} />
          </Suspense>
        )}
      </div>
    </section>
  );
}

/**
 * The server at a glance: what is wrong, the key numbers and the last day's load, with the
 * datacenters, services, checks and WEB transport one click away. `focus` opens one section
 * for a link that used to lead to its own tab.
 */
export function NodeOverviewTab({ node, focus }: { node: Node; focus?: 'checks' | 'web' | null }) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const telemt = node.engine === 'telemt';
  const [range, setRange] = useState<MonitoringRange>('24h');

  const healthQuery = useNodeHealth(node.id, node.online);
  const alertsQuery = useAlerts();
  const diagnostics = useNodeDiagnostics(telemt ? node.id : '', 1);
  const probes = useNodeProbes(node.id);
  const carriers = useNodeWebCarriers(node.id, telemt);
  const series = useNodeSeries(node.id, range);
  const runCheck = useRunNodeCheck(node.id);
  const runDiagnostics = useRunWebDiagnostics(node.id);

  const offline = !node.online || (healthQuery.error instanceof ApiError && healthQuery.error.code === 'node_offline');
  const health = offline ? undefined : healthQuery.data;
  const live: LiveState = offline ? 'offline' : healthQuery.isError ? 'error' : health ? 'ready' : 'loading';
  const points = series.data?.points ?? [];

  const tally = checkTally(diagnostics.data?.items?.[0], node.last_check, freshProbeChecks(probes.data));
  const verdict = nodeVerdict(node, alertsQuery.data?.items ?? [], tally, t, i18n);
  const dcs = dcState(health);
  const services = health ? serviceReadings(node, health, t('nodes.detail_agent')) : [];
  const proxyName = telemt ? 'telemt' : 'relay';
  const running = runCheck.isPending || runDiagnostics.isPending;
  const retryHealth = () => void healthQuery.refetch();

  const runChecks = async () => {
    const jobs: Promise<unknown>[] = [runCheck.mutateAsync()];
    if (telemt && node.online) jobs.push(runDiagnostics.mutateAsync());
    const failed = (await Promise.allSettled(jobs)).find((r): r is PromiseRejectedResult => r.status === 'rejected');
    if (failed) {
      const err: unknown = failed.reason;
      toast.add({ description: err instanceof ApiError ? err.message : t('nodes.check_request_failed'), type: 'error' });
    }
  };

  return (
    <div className={cn(ENTER_CLASS, 'flex flex-col gap-4')}>
      <NodeVerdict verdict={verdict} canInstall={isWriter} />
      <NodeHealthStrip node={node} verdict={verdict} health={health} loading={live === 'loading'} dcs={dcs} checks={tally} />
      <Panel>
        <LoadBlock
          node={node}
          range={range}
          onRange={setRange}
          points={points}
          loading={series.isLoading}
          error={series.isError}
          onRetry={() => void series.refetch()}
        />
        {telemt && (
          <Section id="telegram" title={t('nodes.dcs_title')} summary={dcSummary(live, health, dcs, t)}>
            <SubSections>
              <NodeDcsCard
                engine={node.engine}
                offline={offline}
                health={health}
                error={live === 'error'}
                onRetry={retryHealth}
              />
              <DcHistory points={points} range={range} />
              <ReliabilityRoutes report={health?.reliability} />
            </SubSections>
          </Section>
        )}
        <Section id="services" title={t('nodes.detail_services')} summary={servicesSummary(live, health, services, proxyName, t)}>
          <NodeServicesSection
            node={node}
            health={health}
            offline={offline}
            error={live === 'error'}
            onRetry={retryHealth}
            points={points}
            rangeLabel={t(`monitoring.range_${range}`)}
          />
        </Section>
        <Section
          id="checks"
          title={t('nodes.detail_checks')}
          summary={checksSummary(tally, t)}
          focus={focus === 'checks'}
          action={
            isWriter && (
              <Button type="button" variant="outline" size="sm" onClick={() => void runChecks()} disabled={running}>
                <RefreshCw className={cn(running && 'animate-spin')} />
                {t('nodes.detail_checks_run')}
              </Button>
            )
          }
        >
          <SubSections>
            {telemt && <WebDiagnosticsCard nodeId={node.id} embedded runner={runDiagnostics} />}
            <NodeCheckCard node={node} runner={runCheck} />
            <NodeProbes id={node.id} />
          </SubSections>
        </Section>
        {telemt && (
          <Section
            id="web"
            title={t('web.status_title')}
            summary={webSummary(nodeCapability(node, CAP_WEB), health, carriers.data, t)}
            focus={focus === 'web'}
          >
            <NodeWebSection node={node} />
          </Section>
        )}
      </Panel>
    </div>
  );
}

function DcHistory({ points, range }: { points: SeriesPoint[]; range: MonitoringRange }) {
  const { t } = useTranslation();
  const chart = useMemo(() => dcLatencyRows(points), [points]);
  return (
    <SubSection title={t('nodes.detail_dcs_history', { range: t(`monitoring.range_${range}`) })}>
      {chart.rows.length === 0 ? (
        <p className="text-body text-mute">{t('nodes.dc_latency_empty')}</p>
      ) : (
        <Suspense fallback={<Skeleton className="h-[152px] w-full" />}>
          <DcLatencyChart dcs={chart.dcs} rows={chart.rows} />
        </Suspense>
      )}
    </SubSection>
  );
}
