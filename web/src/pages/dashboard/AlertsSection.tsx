import { useTranslation } from 'react-i18next';

import { useAlerts, useResolveAlert } from '@/api/dashboard';
import { useAuth } from '@/auth/AuthProvider';
import { PanelEmpty } from '@/components/common/EmptyState';
import { PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';
import { formatCompactAge } from '@/lib/format';

/**
 * Open alerts, newest first. Each row is a red dot, the node it happened on,
 * and one mono line giving the machine's own words for what happened plus how
 * long ago - the two things that decide whether this needs attention now. The
 * message itself lives on the node page; a list is for triage, not reading.
 */
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
        title={t('dashboard.alerts_title')}
        meta={alertsQuery.isLoading ? undefined : t('dashboard.alerts_open_count', { count: alerts.length })}
      />

      {alertsQuery.isLoading ? (
        // Two rows in the shape of the list that is coming: a name line and
        // the mono line under it, indented past where the status dot sits.
        <ul className="divide-y divide-hairline" aria-hidden="true">
          {[0, 1].map((i) => (
            <li key={i} className="space-y-2 px-4 py-3">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="ml-[15px] h-3 w-44" />
            </li>
          ))}
        </ul>
      ) : alerts.length === 0 ? (
        <PanelEmpty>{t('dashboard.alerts_empty')}</PanelEmpty>
      ) : (
        <ul className="divide-y divide-hairline">
          {alerts.map((a) => {
            const age = formatCompactAge(a.created_at, i18n.language);
            return (
              <li key={a.id} className="flex items-start justify-between gap-3 px-4 py-3">
                <div className="min-w-0">
                  <p className="flex items-center gap-2 text-body text-foreground">
                    <span className="size-[7px] shrink-0 rounded-pill bg-offline" aria-hidden="true" />
                    <span className="truncate">{a.node_name || t('dashboard.alert_panel_scope')}</span>
                  </p>
                  <p className="mono mt-1 truncate pl-[15px] text-mono text-dim">
                    {a.kind}
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
