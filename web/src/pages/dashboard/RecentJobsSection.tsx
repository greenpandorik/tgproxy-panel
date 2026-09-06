import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { PanelHeader } from '@/components/common/Panel';
import { formatCompactAge, formatCompactDuration } from '@/lib/format';
import { cn } from '@/lib/utils';

import type { ApplyJob } from '@/api/types';

const FAILED_STATUSES = new Set(['failed', 'rolled_back']);

/** How long the apply took, or how long ago it started when it is still running. */
function jobTiming(job: ApplyJob, locale: string): string | null {
  if (job.started_at && job.finished_at) {
    const ms = new Date(job.finished_at).getTime() - new Date(job.started_at).getTime();
    if (Number.isFinite(ms) && ms >= 0) return formatCompactDuration(ms / 1000, locale);
  }
  return formatCompactAge(job.started_at ?? job.created_at, locale);
}

/**
 * The last applies, as a log rather than a card list: node, what was pushed,
 * and how long it took next to the status the machine reported. Statuses stay
 * in their raw form (`ok`, `failed`) and in mono - they are the words that
 * appear in the job log and the audit trail, and translating them here would
 * only make the two harder to match up.
 */
export function RecentJobsSection({ jobs }: { jobs: ApplyJob[] }) {
  const { t, i18n } = useTranslation();

  return (
    <>
      <PanelHeader
        title={t('dashboard.jobs_title')}
        meta={jobs.length > 0 ? String(jobs.length) : undefined}
        className="border-t border-hairline"
      />

      {jobs.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-mute">{t('dashboard.jobs_empty')}</p>
      ) : (
        <ul className="divide-y divide-hairline">
          {jobs.map((job) => {
            const timing = jobTiming(job, i18n.language);
            return (
              <li key={job.id} className="flex items-center justify-between gap-3 px-4 py-2.5">
                <div className="flex min-w-0 items-center gap-2">
                  <Link to={`/nodes/${job.node_id}`} className="truncate text-sm text-foreground hover:underline">
                    {job.node_name || job.node_id}
                  </Link>
                  <span className="mono shrink-0 rounded-sm border border-hairline-strong px-1.5 text-[11px] text-mute">
                    {job.kind}
                  </span>
                </div>
                <span className={cn('mono shrink-0 text-xs', FAILED_STATUSES.has(job.status) ? 'text-err' : 'text-dim')}>
                  {timing && `${timing} · `}
                  {job.status}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </>
  );
}
