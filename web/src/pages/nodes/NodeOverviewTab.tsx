import {
  ChevronDown,
  ChevronRight,
  Cpu,
  ExternalLink,
  HardDrive,
  HeartPulse,
  History,
  MemoryStick,
  Package,
  Timer,
} from 'lucide-react';
import { Fragment, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useNodeHealth, useNodeJobs } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { PanelEmpty } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { StatGrid } from '@/components/common/StatGrid';
import { statTone } from '@/components/common/statTone';
import { ENTER_CLASS } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { ApiError } from '@/lib/api';
import { formatCompactDuration, formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import { NodeCheckCard } from './NodeCheckCard';
import { NodeDcsCard } from './NodeDcsCard';
import { NodeListenersCard } from './NodeListenersCard';
import { DASH, telemtVersion } from './nodeDisplay';

import type { ReactNode } from 'react';
import type { StatGridTile } from '@/components/common/StatGrid';
import type { LucideIcon } from 'lucide-react';
import type { ApplyJob, ApplyStatus, Node, NodeHealth } from '@/api/types';
import type { TFunction } from 'i18next';

const TPROXY_REPO = 'https://github.com/telegramdesktop/tproxy-server';

const FAILED_STATUSES = new Set<ApplyStatus>(['failed', 'rolled_back']);

function isCommitHash(v: string): boolean {
  return /^[0-9a-f]{7,40}$/i.test(v);
}

/**
 * One cell of a mono slab: what it is on the left, what the machine says on
 * the right. A grid of these replaces a row of bordered boxes - the point of
 * a health readout is that the values line up in a column you can sweep, and
 * boxes put a frame between every pair of them.
 */
function Cell({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div className={cn('flex items-center justify-between gap-3 bg-card px-4 py-3', className)}>
      <span className="truncate text-label text-mute">{label}</span>
      <span className="mono shrink-0 text-mono">{children}</span>
    </div>
  );
}

/** A service's state as the panel's standard dot + word, in the machine's own vocabulary. */
function ServiceState({ active }: { active: boolean }) {
  const { t } = useTranslation();
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={cn('size-[7px] shrink-0 rounded-pill', active ? 'bg-ok' : 'bg-err')} aria-hidden="true" />
      <span className={active ? 'text-mute' : 'text-err'}>{t(active ? 'nodes.state_up' : 'nodes.state_down')}</span>
    </span>
  );
}

/**
 * The four figures the health readout ends on: how long the node has been up,
 * and how much of its processor, memory and disk is in use.
 *
 * They are the panel's stat tiles rather than four hand-drawn rules, so a
 * number here is shaped exactly like a number on the dashboard. Uptime is a
 * fact that cannot be good or bad and stays neutral; the three resources take
 * `statTone`'s one load rule, quiet under 80%, amber at 80 and red at 95, so a
 * node's own reading and the fleet average that includes it cannot disagree
 * about when a machine is under pressure.
 */
/**
 * Two columns then four, never three: the block is exactly four tiles, so the
 * grid's default third column would leave disk alone on a second row for the
 * whole tablet range.
 */
const HEALTH_GRID = 'border-t border-hairline p-4 sm:grid-cols-2 lg:grid-cols-4';

function healthTiles(health: NodeHealth | undefined, t: TFunction, language: string): StatGridTile[] {
  // While the readout is still on its way the tiles are the same four boxes
  // with skeletons in them, and every plate stays neutral: a tone is a claim
  // about the machine, and nothing has been reported yet.
  const loading = !health;
  const usage = (percent: number | undefined) => Math.max(0, Math.min(100, percent ?? 0));
  const resource = (id: string, icon: LucideIcon, label: string, percent: number | undefined): StatGridTile => ({
    id,
    icon,
    tone: loading ? 'neutral' : statTone({ kind: 'avg_load', percent: usage(percent) }),
    label,
    value: usage(percent).toFixed(0),
    unit: '%',
    loading,
  });

  return [
    {
      id: 'uptime',
      icon: Timer,
      tone: statTone({ kind: 'stateless' }),
      label: t('nodes.overview_uptime'),
      value: formatCompactDuration(health?.uptime_seconds ?? 0, language),
      loading,
    },
    resource('cpu', Cpu, t('nodes.overview_cpu'), health?.cpu_percent),
    resource('mem', MemoryStick, t('nodes.overview_mem'), health?.mem_used_percent),
    resource('disk', HardDrive, t('nodes.overview_disk'), health?.disk_used_percent),
  ];
}

/**
 * The slots that keep the last row of the service grid whole.
 *
 * The 1px gaps between the cells are painted by the container, so a row that
 * runs out of cells shows the gap colour as a lit rectangle. telemt serves
 * Fake-TLS itself and reports mtproxy_active as a constant true, so its grid
 * is four cells rather than five: it needs two fillers at three columns and
 * none at two, where a tproxy node's five need the one filler either way.
 */
function ServiceGridFiller({ telemt }: { telemt: boolean }) {
  if (!telemt) return <div className="hidden bg-card sm:block" aria-hidden="true" />;
  return (
    <>
      <div className="hidden bg-card lg:block" aria-hidden="true" />
      <div className="hidden bg-card lg:block" aria-hidden="true" />
    </>
  );
}

/**
 * The health readout before it arrives: the same service grid and the same
 * four tiles under it, drawn empty. The panel does not change height when the
 * figures land.
 */
function HealthSkeleton({ telemt }: { telemt: boolean }) {
  const { t, i18n } = useTranslation();
  return (
    <>
      <div className="grid grid-cols-1 gap-px bg-hairline sm:grid-cols-2 lg:grid-cols-3">
        {Array.from({ length: telemt ? 4 : 5 }).map((_, i) => (
          <div key={i} className="flex items-center justify-between gap-3 bg-card px-4 py-3">
            <Skeleton className="h-3 w-24" />
            <Skeleton className="h-3 w-14" />
          </div>
        ))}
        <ServiceGridFiller telemt={telemt} />
      </div>
      <StatGrid tiles={healthTiles(undefined, t, i18n.language)} className={HEALTH_GRID} />
    </>
  );
}

/** What went wrong, and the one button that tries again. */
function PanelError({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation();
  return <ErrorState inset message={t('common.error_generic')} retryLabel={t('common.refresh')} onRetry={onRetry} />;
}

function JobRow({ job }: { job: ApplyJob }) {
  const { t, i18n } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const failed = FAILED_STATUSES.has(job.status);

  return (
    <Fragment>
      <TableRow className="cursor-pointer" onClick={() => setExpanded((e) => !e)} aria-expanded={expanded}>
        <TableCell className="w-0 pr-0 text-mute">
          {expanded ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
        </TableCell>
        <TableCell className="mono text-mono text-mute">{t(`nodes.job_kind_${job.kind}`, job.kind)}</TableCell>
        <TableCell className="mono text-mono text-mute">{formatDateTime(job.created_at, i18n.language)}</TableCell>
        <TableCell className="mono text-right text-mono text-mute">
          {job.started_at && job.finished_at
            ? formatCompactDuration(
                (new Date(job.finished_at).getTime() - new Date(job.started_at).getTime()) / 1000,
                i18n.language,
              )
            : DASH}
        </TableCell>
        <TableCell className={cn('mono text-right text-mono', failed ? 'text-err' : 'text-mute')}>{job.status}</TableCell>
      </TableRow>
      {expanded && (
        <TableRow className="hover:bg-transparent">
          <TableCell colSpan={5} className="h-auto py-3 pl-9 whitespace-normal">
            <div className="mono flex flex-wrap gap-x-4 gap-y-1 text-mono text-mute">
              <span>
                {t('nodes.job_started')}: {job.started_at ? formatDateTime(job.started_at, i18n.language) : DASH}
              </span>
              <span>
                {t('nodes.job_finished')}: {job.finished_at ? formatDateTime(job.finished_at, i18n.language) : DASH}
              </span>
            </div>
            {job.error && <p className="mono mt-2 text-mono text-err">{job.error}</p>}
            <pre className="mono mt-2 max-h-64 overflow-auto rounded-surface border border-hairline bg-background p-3 text-mono whitespace-pre-wrap text-mute">
              {job.log?.trim() ? job.log : t('nodes.job_log_empty')}
            </pre>
          </TableCell>
        </TableRow>
      )}
    </Fragment>
  );
}

/**
 * What the node is doing right now, in four panels. The states it reports keep
 * the slab shape - a label on the left, the machine's own value in mono on the
 * right - and the figures it reports are stat tiles, the same as everywhere
 * else in the panel. Colour appears only where something is wrong: a dead
 * service, a resource past its threshold, a failed apply. Anything toned on
 * this page is the thing to look at.
 */
export function NodeOverviewTab({ node }: { node: Node }) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const healthQuery = useNodeHealth(node.id, node.online);
  const jobsQuery = useNodeJobs(node.id);
  const telemt = node.engine === 'telemt';

  const offline = !node.online || (healthQuery.error instanceof ApiError && healthQuery.error.code === 'node_offline');
  const health = healthQuery.data;
  const jobs = jobsQuery.data?.items ?? [];

  return (
    <div className={cn(ENTER_CLASS, 'flex flex-col gap-4')}>
      <Panel>
        <PanelHeader icon={HeartPulse} title={t('nodes.overview_health')} />
        {offline ? (
          <PanelEmpty>{t('nodes.offline_message')}</PanelEmpty>
        ) : healthQuery.isError ? (
          <PanelError onRetry={() => void healthQuery.refetch()} />
        ) : !health ? (
          <HealthSkeleton telemt={telemt} />
        ) : (
          <>
            <div className="grid grid-cols-1 gap-px bg-hairline sm:grid-cols-2 lg:grid-cols-3">
              <Cell label={t(telemt ? 'nodes.overview_telemt_active' : 'nodes.overview_relay_active')}>
                <ServiceState active={health.relay_active} />
              </Cell>
              {/* telemt serves Fake-TLS itself and reports mtproxy_active as a constant
                  true - a row that can never say anything is noise, so it is omitted. */}
              {!telemt && (
                <Cell label={t('nodes.overview_mtproxy_active')}>
                  <ServiceState active={health.mtproxy_active} />
                </Cell>
              )}
              <Cell label={t('nodes.overview_caddy_active')}>
                <ServiceState active={health.caddy_active} />
              </Cell>
              <Cell label={t('nodes.overview_healthz')}>
                <ServiceState active={health.healthz} />
              </Cell>
              <Cell label={t('nodes.overview_readyz')}>
                <ServiceState active={health.readyz} />
              </Cell>
              <ServiceGridFiller telemt={telemt} />
            </div>

            {/* Uptime leaves the service grid and joins the three resource
                figures: the four of them are the numbers on this page, and a
                number belongs in a tile rather than in a row of yes/no states. */}
            <StatGrid tiles={healthTiles(health, t, i18n.language)} className={HEALTH_GRID} />
          </>
        )}
      </Panel>

      {/* The same health readout, read for its other half: how the node reaches
          Telegram. It follows the services panel because the two answer the
          same question in order - is the proxy up, and can it get through. */}
      <NodeDcsCard
        engine={node.engine}
        offline={offline}
        health={health}
        error={healthQuery.isError}
        onRetry={() => void healthQuery.refetch()}
      />

      <Panel>
        <PanelHeader icon={Package} title={t('nodes.overview_versions')} />
        <div className="grid grid-cols-1 gap-px bg-hairline sm:grid-cols-3">
          {telemt ? (
            <Cell label={t('nodes.overview_telemt_version')}>
              <span className={telemtVersion(node) ? 'text-mute' : 'text-dim'}>{telemtVersion(node) || DASH}</span>
            </Cell>
          ) : (
            <Cell label={t('nodes.overview_tproxy_version')}>
              {node.tproxy_version ? (
                isCommitHash(node.tproxy_version) ? (
                  <a
                    href={`${TPROXY_REPO}/commit/${node.tproxy_version}`}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 text-brand-ink underline-offset-3 hover:underline"
                  >
                    {node.tproxy_version.slice(0, 12)}
                    <ExternalLink className="size-3" aria-hidden="true" />
                  </a>
                ) : (
                  <span className="text-mute">{node.tproxy_version}</span>
                )
              ) : (
                <span className="text-dim">{DASH}</span>
              )}
            </Cell>
          )}
          <Cell label={t('nodes.overview_agent_version')}>
            <span className={node.agent_version ? 'text-mute' : 'text-dim'}>{node.agent_version || DASH}</span>
          </Cell>
          <Cell label={t('nodes.overview_last_apply')}>
            <span className="text-mute">
              {node.last_apply_at ? formatDateTime(node.last_apply_at, i18n.language) : t('nodes.overview_never')}
            </span>
          </Cell>
        </div>
      </Panel>

      {telemt && <NodeListenersCard node={node} canEdit={isWriter} />}

      <NodeCheckCard node={node} />

      <Panel>
        <PanelHeader
          icon={History}
          title={t('nodes.overview_jobs_title')}
          meta={jobs.length > 0 ? String(jobs.length) : undefined}
        />
        {jobsQuery.isLoading ? (
          <div className="divide-y divide-hairline">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex h-11 items-center gap-4 px-3">
                <Skeleton className="h-3 w-24" />
                <Skeleton className="h-3 w-36" />
                <Skeleton className="ml-auto h-3 w-12" />
                <Skeleton className="h-3 w-16" />
              </div>
            ))}
          </div>
        ) : jobsQuery.isError ? (
          <PanelError onRetry={() => void jobsQuery.refetch()} />
        ) : jobs.length === 0 ? (
          <PanelEmpty>{t('nodes.overview_jobs_empty')}</PanelEmpty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-0" />
                <TableHead>{t('nodes.job_column_kind')}</TableHead>
                <TableHead>{t('nodes.job_column_created')}</TableHead>
                <TableHead className="text-right">{t('nodes.job_column_duration')}</TableHead>
                <TableHead className="text-right">{t('nodes.job_column_status')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {jobs.map((job) => (
                <JobRow key={job.id} job={job} />
              ))}
            </TableBody>
          </Table>
        )}
      </Panel>
    </div>
  );
}
