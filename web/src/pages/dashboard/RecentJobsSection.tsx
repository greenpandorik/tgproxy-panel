import { History } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { Badge } from '@/components/ui/badge';
import { PanelEmpty } from '@/components/common/EmptyState';
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
        icon={History}
        title={t('dashboard.jobs_title')}
        meta={jobs.length > 0 ? String(jobs.length) : undefined}
        className="border-t border-hairline"
      />

      {jobs.length === 0 ? (
        <PanelEmpty>{t('dashboard.jobs_empty')}</PanelEmpty>
      ) : (
        <ul className="divide-y divide-hairline">
          {jobs.map((job) => {
            const timing = jobTiming(job, i18n.language);
            return (
              <li key={job.id} className="flex items-center justify-between gap-3 px-4 py-2">
                <div className="flex min-w-0 items-center gap-2">
                  <Link to={`/nodes/${job.node_id}`} className="truncate text-body text-foreground hover:underline">
                    {job.node_name || job.node_id}
                  </Link>
                  <Badge>{job.kind}</Badge>
                </div>
                <span className={cn('mono shrink-0 text-mono', FAILED_STATUSES.has(job.status) ? 'text-err' : 'text-mute')}>
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
