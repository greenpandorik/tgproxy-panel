import { Bell } from 'lucide-react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

import { useAlerts, useResolveAlert } from '@/api/dashboard';
import { useAuth } from '@/auth/AuthProvider';
import { PanelEmpty } from '@/components/common/EmptyState';
import { PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { ErrorState } from '@/components/common/ErrorState';
import { ApiError } from '@/lib/api';
import { formatCompactAge } from '@/lib/format';

// Open alerts, newest first.
export function AlertsSection() {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const alertsQuery = useAlerts();
  const resolveAlert = useResolveAlert();

  const alerts = alertsQuery.data?.items ?? [];

  const handleResolve = async (id: number) => {
    try {
      await resolveAlert.mutateAsync(id);
      toast.add({ description: t('dashboard.alert_resolved'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <>
      <PanelHeader
        icon={Bell}
        title={t('dashboard.alerts_title')}
        meta={alertsQuery.isLoading ? undefined : t('dashboard.alerts_open_count', { count: alerts.length })}
      />

      {alertsQuery.isLoading ? (
        <ul className="divide-y divide-hairline" aria-hidden="true">
          {[0, 1].map((i) => (
            <li key={i} className="space-y-2 px-4 py-3">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="ml-[15px] h-3 w-44" />
            </li>
          ))}
        </ul>
      ) : alertsQuery.isError ? (
        <ErrorState
          message={t('common.error_generic')}
          retryLabel={t('common.refresh')}
          onRetry={() => void alertsQuery.refetch()}
        />
      ) : alerts.length === 0 ? (
        <PanelEmpty>{t('dashboard.alerts_empty')}</PanelEmpty>
      ) : (
        <ul className="divide-y divide-hairline">
          {alerts.map((a) => {
            const age = formatCompactAge(a.created_at, i18n.language);
            return (
              <li key={a.id} className="flex flex-wrap items-start justify-between gap-3 px-4 py-3">
                <div className="min-w-0">
                  <p className="flex items-center gap-2 text-body text-foreground">
                    <span className="size-[7px] shrink-0 rounded-pill bg-offline" aria-hidden="true" />
                    {a.node_id ? (
                      <Link to={`/nodes/${a.node_id}`} className="truncate font-medium hover:underline">
                        {a.node_name || t('dashboard.alert_panel_scope')}
                      </Link>
                    ) : (
                      <span>{t('dashboard.alert_panel_scope')}</span>
                    )}
                  </p>
                  <p className="mt-1 pl-[15px] text-label text-mute">
                    {t(`dashboard.alert_${a.kind}`, { defaultValue: a.message || t('dashboard.alert_unknown') })}
                    {age && ` · ${t('common.ago', { value: age })}`}
                  </p>
                </div>
                {isWriter && (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="shrink-0"
                    onClick={() => void handleResolve(a.id)}
                    disabled={resolveAlert.isPending}
                  >
                    {t('dashboard.alert_resolve')}
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </>
  );
}
