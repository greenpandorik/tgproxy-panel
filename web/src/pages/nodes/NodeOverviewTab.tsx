import { ChevronDown, ChevronRight, ExternalLink } from 'lucide-react';
import { Fragment, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useNodeHealth, useNodeJobs } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { PanelEmpty } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { ENTER_CLASS } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { ApiError } from '@/lib/api';
import { formatCompactDuration, formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import { NodeCheckCard } from './NodeCheckCard';
import { NodeListenersCard } from './NodeListenersCard';
import { DASH, telemtVersion } from './nodeDisplay';

import type { ReactNode } from 'react';
import type { ApplyJob, ApplyStatus, Node } from '@/api/types';

const TPROXY_REPO = 'https://github.com/telegramdesktop/tproxy-server';

const FAILED_STATUSES = new Set<ApplyStatus>(['failed', 'rolled_back']);

function isCommitHash(v: string): boolean {
  return /^[0-9a-f]{7,40}$/i.test(v);
}

/**
 * One cell of a mono slab: what it is on the left, what the machine says on
 * the right. Six of these in a grid replace six bordered boxes - the point of
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

/** Percentage of a resource, as a figure over the same 3px rule the capacity column uses. */
function UsageBar({ label, percent }: { label: string; percent: number }) {
  const pct = Math.max(0, Math.min(100, percent));
  const tone = pct >= 90 ? 'bg-err' : pct >= 70 ? 'bg-warn' : 'bg-brand-primary';
  return (
    <div className="min-w-0">
      <div className="flex items-baseline justify-between gap-2">
        <span className="truncate text-label text-mute">{label}</span>
        <span className="mono shrink-0 text-mono text-foreground">{pct.toFixed(0)}%</span>
      </div>
      <div className="mt-2 h-[3px] w-full overflow-hidden rounded-pill bg-hairline">
        <div className={cn('h-full', tone)} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

/**
 * The health readout before it arrives: the same cell grid and the same three
 * rules under it, drawn empty. The panel does not change height when the
 * figures land.
 */
function HealthSkeleton({ telemt }: { telemt: boolean }) {
  return (
    <>
      <div className="grid grid-cols-1 gap-px bg-hairline sm:grid-cols-2 lg:grid-cols-3">
        {Array.from({ length: telemt ? 5 : 6 }).map((_, i) => (
          <div key={i} className="flex items-center justify-between gap-3 bg-card px-4 py-3">
            <Skeleton className="h-3 w-24" />
            <Skeleton className="h-3 w-14" />
          </div>
        ))}
        {/* Same filler as the readout: the 1px gaps are painted by the
            container, so an empty slot would read as a lit rectangle. */}
        {telemt && <div className="hidden bg-card sm:block" aria-hidden="true" />}
      </div>
      <div className="grid grid-cols-1 gap-4 border-t border-hairline p-4 sm:grid-cols-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i}>
            <div className="flex items-baseline justify-between gap-2">
              <Skeleton className="h-3 w-16" />
              <Skeleton className="h-3 w-8" />
            </div>
            <Skeleton className="mt-2 h-[3px] w-full rounded-pill" />
          </div>
        ))}
      </div>
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
        <TableCell className="w-0 pr-0 text-dim">
          {expanded ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
        </TableCell>
        <TableCell className="mono text-mono text-mute">{t(`nodes.job_kind_${job.kind}`, job.kind)}</TableCell>
        <TableCell className="mono text-mono text-dim">{formatDateTime(job.created_at, i18n.language)}</TableCell>
        <TableCell className="mono text-right text-mono text-dim">
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
            <div className="mono flex flex-wrap gap-x-4 gap-y-1 text-mono text-dim">
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
 * What the node is doing right now, in four panels that all speak the same
 * shape: a label on the left, the machine's own value in mono on the right.
 * Colour appears only where something is wrong - a dead service, a resource
 * over 90%, a failed apply - so anything red on this page is the thing to look
 * at.
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
        <PanelHeader title={t('nodes.overview_health')} />
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
              <Cell label={t('nodes.overview_uptime')} className="text-foreground">
                {formatCompactDuration(health.uptime_seconds, i18n.language)}
              </Cell>
              {/* Dropping the MTProxy row leaves five cells in a two- or three-column
                  grid, and the 1px gaps are painted by the container - so the empty
                  slot would read as a lit rectangle. Fill it with the panel surface. */}
              {telemt && <div className="hidden bg-card sm:block" aria-hidden="true" />}
            </div>

            <div className="grid grid-cols-1 gap-4 border-t border-hairline p-4 sm:grid-cols-3">
              <UsageBar label={t('nodes.overview_cpu')} percent={health.cpu_percent} />
              <UsageBar label={t('nodes.overview_mem')} percent={health.mem_used_percent} />
              <UsageBar label={t('nodes.overview_disk')} percent={health.disk_used_percent} />
            </div>
          </>
        )}
      </Panel>

      <Panel>
        <PanelHeader title={t('nodes.overview_versions')} />
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
            <span className={node.last_apply_at ? 'text-mute' : 'text-dim'}>
              {node.last_apply_at ? formatDateTime(node.last_apply_at, i18n.language) : t('nodes.overview_never')}
            </span>
          </Cell>
        </div>
      </Panel>

      {telemt && <NodeListenersCard node={node} canEdit={isWriter} />}

      <NodeCheckCard node={node} />

      <Panel>
        <PanelHeader title={t('nodes.overview_jobs_title')} meta={jobs.length > 0 ? String(jobs.length) : undefined} />
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
