import { Server, UserPlus } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';

import { useDashboardSummary, useDashboardTrends } from '@/api/dashboard';
import { useNodes } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { EmptyState } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { PageHeader } from '@/components/common/PageHeader';
import { Button } from '@/components/ui/button';
import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { formatCompactAge } from '@/lib/format';

import { AttentionInbox } from './dashboard/AttentionInbox';
import { DashboardMetrics } from './dashboard/DashboardMetrics';
import { HistorySection } from './dashboard/HistorySection';
import { ServerCards } from './dashboard/ServerCards';

const RECENT_JOBS_LIMIT = 6;

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

function CardsSkeleton() {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,260px),1fr))] gap-3" aria-hidden="true">
      {[0, 1, 2].map((i) => (
        <Skeleton key={i} className="h-36 w-full" />
      ))}
    </div>
  );
}

export function DashboardPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { isWriter } = useAuth();

  const summaryQuery = useDashboardSummary();
  const nodesQuery = useNodes();
  const trendsQuery = useDashboardTrends();

  const summary = summaryQuery.data;
  const trends = trendsQuery.data;
  const nodes = nodesQuery.data?.items ?? [];
  const loading = summaryQuery.isLoading || nodesQuery.isLoading;
  const failed = summaryQuery.isError || nodesQuery.isError;
  const header = <PageHeader title={t('dashboard.title')} actions={<HelpButton topic="dashboard" />} />;

  if (!loading && failed) {
    const error = summaryQuery.error ?? nodesQuery.error;
    return (
      <>
        {header}
        <ErrorState
          message={error instanceof ApiError ? error.message : t('common.error_generic')}
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
        {header}
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
        description={
          <>
            {t('workspace.home_hint')} <UpdatedAgo at={summaryQuery.dataUpdatedAt} />
          </>
        }
        actions={
          <>
            <HelpButton topic="dashboard" />
            {isWriter && (
              <Button nativeButton={false} render={<Link to="/users?create=1" />}>
                <UserPlus />
                {t('dashboard.manage_access')}
              </Button>
            )}
          </>
        }
      />

      <div className={`${ENTER_CLASS} grid grid-cols-1 gap-3 lg:grid-cols-[minmax(0,7fr)_minmax(0,5fr)]`} style={enterDelay(0)}>
        <AttentionInbox />
        <DashboardMetrics
          loading={loading}
          nodesOnline={summary?.nodes.online}
          nodesTotal={summary?.nodes.total}
          nodesDown={nodes.filter((n) => n.status === 'offline').map((n) => n.name)}
          keysActive={summary?.keys.active}
          keysTotal={summary ? summary.keys.total - summary.keys.revoked : undefined}
          people={summary?.people_online}
          people15m={summary?.people_online_15m}
          peopleSeries={(trends?.people ?? []).map((p) => p.people_online)}
          connections={summary?.sessions_live}
          traffic={trends?.traffic_24h}
          trafficBefore={trends?.traffic_prev_24h}
        />
      </div>

      <section aria-labelledby="servers-title" className={`${ENTER_CLASS} space-y-3`} style={enterDelay(1)}>
        <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
          <h2 id="servers-title" className="text-title text-foreground">
            {t('nodes.title')}
          </h2>
          {isWriter && nodes.length > 1 && <p className="text-label text-mute">{t('nodes.reorder_hint_cards')}</p>}
        </div>
        {loading ? <CardsSkeleton /> : <ServerCards nodes={nodes} writer={isWriter} />}
      </section>

      <HistorySection nodes={nodes} jobs={(summary?.recent_jobs ?? []).slice(0, RECENT_JOBS_LIMIT)} />
    </>
  );
}
