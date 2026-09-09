import { CircleCheck, TriangleAlert } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { useAlerts, useResolveAlert } from '@/api/dashboard';
import { useApplyNode, useRestartNode } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { LoadingState } from '@/components/common/PageState';
import { StatusIndicator } from '@/components/common/StatusIndicator';
import { Button } from '@/components/ui/button';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';
import { formatCompactAge } from '@/lib/format';

import type { Alert } from '@/api/types';
import type { HealthStatus } from '@/components/common/healthStatus';

/** The kinds the backend actually raises, and the state each one puts a node in. */
const ALERT_STATUS: Record<string, HealthStatus> = {
  node_offline: 'offline',
  node_degraded: 'degraded',
  apply_failed: 'degraded',
  config_deferred: 'degraded',
};

function AttentionRow({ alert }: { alert: Alert }) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const nodeId = alert.node_id ?? '';

  const applyNode = useApplyNode(nodeId);
  const restartNode = useRestartNode(nodeId);
  const resolveAlert = useResolveAlert();
  const [restartOpen, setRestartOpen] = useState(false);

  const status = ALERT_STATUS[alert.kind] ?? 'unknown';
  const age = formatCompactAge(alert.created_at, i18n.language);
  const what = t(`dashboard.alert_${alert.kind}`, { defaultValue: alert.message || t('dashboard.alert_unknown') });

  const run = async (action: () => Promise<unknown>, success: string) => {
    try {
      await action();
      toast.add({ description: success, type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const fix = () => {
    if (!nodeId) return null;
    if (isWriter && alert.kind === 'apply_failed') {
      return (
        <Button
          type="button"
          size="sm"
          disabled={applyNode.isPending}
          onClick={() => void run(() => applyNode.mutateAsync(), t('nodes.apply_queued'))}
        >
          {t('dashboard.attention_action_apply')}
        </Button>
      );
    }
    if (isWriter && alert.kind === 'config_deferred') {
      return (
        <Button type="button" size="sm" disabled={restartNode.isPending} onClick={() => setRestartOpen(true)}>
          {t('dashboard.attention_action_restart')}
        </Button>
      );
    }
    return (
      <Button size="sm" variant="outline" nativeButton={false} render={<Link to={`/nodes/${nodeId}`} />}>
        {t('dashboard.attention_action_open')}
      </Button>
    );
  };

  return (
    <li className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2 px-5 py-3.5">
      <div className="min-w-0">
        <p className="flex items-center gap-2 text-body font-medium text-foreground">
          <StatusIndicator status={status} hideLabel />
          {alert.node_id ? (
            <Link to={`/nodes/${alert.node_id}`} className="truncate hover:underline">
              {alert.node_name || t('dashboard.alert_panel_scope')}
            </Link>
          ) : (
            <span className="truncate">{t('dashboard.alert_panel_scope')}</span>
          )}
        </p>
        <p className="mt-1 pl-6 text-label text-mute">
          {what}
          {age && ` · ${t('common.ago', { value: age })}`}
        </p>
      </div>

      <div className="flex shrink-0 items-center gap-2">
        {fix()}
        {isWriter && (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            disabled={resolveAlert.isPending}
            onClick={() => void run(() => resolveAlert.mutateAsync(alert.id), t('dashboard.alert_resolved'))}
          >
            {t('dashboard.alert_resolve')}
          </Button>
        )}
      </div>

      <ConfirmDialog
        open={restartOpen}
        onOpenChange={setRestartOpen}
        title={t('nodes.restart_confirm_title', { name: alert.node_name || t('dashboard.alert_panel_scope') })}
        description={t('nodes.restart_confirm_description')}
        confirmLabel={t('dashboard.attention_action_restart')}
        onConfirm={() => run(() => restartNode.mutateAsync(), t('nodes.restart_success'))}
      />
    </li>
  );
}

/** What is wrong right now, in plain words, each with the action that fixes it. */
export function AttentionSection() {
  const { t } = useTranslation();
  const alertsQuery = useAlerts();
  const alerts = alertsQuery.data?.items ?? [];

  if (alertsQuery.isLoading) {
    return (
      <Panel>
        <PanelHeader icon={TriangleAlert} title={t('dashboard.alerts_title')} />
        <LoadingState inset rows={2} label={t('dashboard.alerts_title')} />
      </Panel>
    );
  }

  if (alertsQuery.isError) {
    return (
      <Panel>
        <PanelHeader icon={TriangleAlert} title={t('dashboard.alerts_title')} />
        <ErrorState
          message={t('dashboard.attention_unavailable')}
          retryLabel={t('common.refresh')}
          onRetry={() => void alertsQuery.refetch()}
        />
      </Panel>
    );
  }

  if (alerts.length === 0) {
    return (
      <p className="flex items-center gap-2 px-1 text-body text-mute">
        <CircleCheck size={15} strokeWidth={1.8} className="shrink-0 text-ok" aria-hidden="true" />
        {t('dashboard.attention_none')}
      </p>
    );
  }

  return (
    <Panel>
      <PanelHeader
        icon={TriangleAlert}
        title={t('dashboard.alerts_title')}
        meta={t('dashboard.alerts_open_count', { count: alerts.length })}
      />
      <ul className="divide-y divide-hairline">
        {alerts.map((alert) => (
          <AttentionRow key={alert.id} alert={alert} />
        ))}
      </ul>
    </Panel>
  );
}
