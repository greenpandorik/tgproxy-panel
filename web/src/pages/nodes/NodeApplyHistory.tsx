import { ChevronDown, ChevronRight } from 'lucide-react';
import { Fragment, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { ErrorState } from '@/components/common/ErrorState';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatCompactDuration, formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import { DASH } from './nodeDisplay';
import { applyFailed } from './nodeHealth';

import type { ApplyJob } from '@/api/types';

function JobRow({ job }: { job: ApplyJob }) {
  const { t, i18n } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const failed = applyFailed(job.status);

  return (
    <Fragment>
      <TableRow>
        <TableCell className="w-0 pr-0 text-mute">
          <button
            type="button"
            className="flex size-9 items-center justify-center rounded-control border border-hairline-strong hover:bg-elevated focus-visible:outline-2 focus-visible:outline-ring"
            aria-expanded={expanded}
            aria-label={t('nodes.job_details')}
            onClick={() => setExpanded((e) => !e)}
          >
            {expanded ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          </button>
        </TableCell>
        <TableCell className="text-mute">{t(`nodes.job_kind_${job.kind}`, job.kind)}</TableCell>
        <TableCell className="mono text-mono text-mute">{formatDateTime(job.created_at, i18n.language)}</TableCell>
        <TableCell className="mono text-right text-mono text-mute">
          {job.started_at && job.finished_at
            ? formatCompactDuration(
                (new Date(job.finished_at).getTime() - new Date(job.started_at).getTime()) / 1000,
                i18n.language,
              )
            : DASH}
        </TableCell>
        <TableCell className={cn('text-right', failed ? 'text-err' : 'text-mute')}>
          {t(`nodes.job_status_${job.status}`, job.status)}
        </TableCell>
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

/** Every apply sent to the server, newest first, each opening onto its log. */
export function NodeApplyHistory({
  jobs,
  loading,
  error,
  onRetry,
}: {
  jobs: ApplyJob[];
  loading: boolean;
  error: boolean;
  onRetry: () => void;
}) {
  const { t } = useTranslation();
  if (loading) return <Skeleton className="h-24 w-full" />;
  if (error) return <ErrorState inset message={t('common.error_generic')} retryLabel={t('common.refresh')} onRetry={onRetry} />;
  if (jobs.length === 0) return <p className="text-body text-mute">{t('nodes.overview_jobs_empty')}</p>;
  return (
    <div className="overflow-hidden rounded-control border border-hairline">
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
    </div>
  );
}
