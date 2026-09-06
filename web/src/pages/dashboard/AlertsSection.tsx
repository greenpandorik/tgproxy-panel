import { useTranslation } from 'react-i18next';

import { useAlerts, useResolveAlert } from '@/api/dashboard';
import { useAuth } from '@/auth/AuthProvider';
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
        <div className="space-y-2 px-4 py-3">
          <Skeleton className="h-4 w-32" />
          <Skeleton className="h-3 w-44" />
        </div>
      ) : alerts.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-mute">{t('dashboard.alerts_empty')}</p>
      ) : (
        <ul className="divide-y divide-hairline">
          {alerts.map((a) => {
            const age = formatCompactAge(a.created_at, i18n.language);
            return (
              <li key={a.id} className="flex items-start justify-between gap-3 px-4 py-3">
                <div className="min-w-0">
                  <p className="flex items-center gap-2 text-sm text-foreground">
                    <span className="size-[7px] shrink-0 rounded-full bg-offline" aria-hidden="true" />
                    <span className="truncate">{a.node_name || t('dashboard.alert_panel_scope')}</span>
                  </p>
                  <p className="mono mt-0.5 truncate pl-[15px] text-xs text-dim">
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
