import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { useApplyNode, useRestartNode } from '@/api/nodes';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { TONE_VAR } from '@/components/common/statTone';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';
import { formatCompactAge } from '@/lib/format';

import { alertTitle } from './alertTitle';

import type { Alert } from '@/api/types';
import type { StatTone } from '@/components/common/statTone';

/** A server that stopped answering is a fault; everything else waits for a person. */
function alertTone(kind: string): StatTone {
  return kind === 'node_offline' ? 'err' : 'warn';
}

/** The problem in plain words, with the server it is about. */
function InboxTitle({ alert }: { alert: Alert }) {
  const { t, i18n } = useTranslation();
  const what = alertTitle(alert, t, i18n);
  if (!alert.node_id) return <>{what}</>;
  const node = alert.node_name || t('dashboard.alert_panel_scope');
  if (alert.kind === 'node_offline') return <>{t('dashboard.inbox_offline', { node })}</>;
  return (
    <>
      <span className="font-semibold">{node}</span>
      <span className="text-mute" aria-hidden="true">
        {' · '}
      </span>
      <span className="sr-only">: </span>
      {what}
    </>
  );
}

interface InboxItemProps {
  alert: Alert;
  /** Writers get a checkbox and the read button. */
  writer: boolean;
  selected: boolean;
  onSelect: (selected: boolean) => void;
  onRead: () => void;
  reading: boolean;
}

/** The move that fixes the problem: apply again, restart, or open the server. */
function FixAction({ alert, nodeId, writer }: { alert: Alert; nodeId: string; writer: boolean }) {
  const { t } = useTranslation();
  const applyNode = useApplyNode(nodeId);
  const restartNode = useRestartNode(nodeId);
  const [restartOpen, setRestartOpen] = useState(false);

  if (!nodeId) return null;

  const run = async (action: () => Promise<unknown>, success: string) => {
    try {
      await action();
      toast.add({ description: success, type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  if (writer && alert.kind === 'apply_failed') {
    return (
      <Button
        type="button"
        size="sm"
        variant="outline"
        disabled={applyNode.isPending}
        onClick={() => void run(() => applyNode.mutateAsync(), t('nodes.apply_queued'))}
      >
        {t('dashboard.attention_action_apply')}
      </Button>
    );
  }
  if (writer && alert.kind === 'config_deferred') {
    return (
      <>
        <Button type="button" size="sm" variant="outline" disabled={restartNode.isPending} onClick={() => setRestartOpen(true)}>
          {t('dashboard.attention_action_restart')}
        </Button>
        <ConfirmDialog
          open={restartOpen}
          onOpenChange={setRestartOpen}
          title={t('nodes.restart_confirm_title', { name: alert.node_name || t('dashboard.alert_panel_scope') })}
          description={t('nodes.restart_confirm_description')}
          confirmLabel={t('dashboard.attention_action_restart')}
          onConfirm={() => run(() => restartNode.mutateAsync(), t('nodes.restart_success'))}
        />
      </>
    );
  }
  return (
    <Button size="sm" variant="outline" nativeButton={false} render={<Link to={`/nodes/${nodeId}`} />}>
      {t('dashboard.attention_action_open')}
    </Button>
  );
}

/** One problem: what it is, how old, the action that fixes it, and «Прочитано». */
export function InboxItem({ alert, writer, selected, onSelect, onRead, reading }: InboxItemProps) {
  const { t, i18n } = useTranslation();
  const nodeId = alert.node_id ?? '';
  const age = formatCompactAge(alert.created_at, i18n.language);
  const titleId = `inbox-alert-${alert.id}`;

  return (
    <li
      data-testid="inbox-item"
      className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-3 gap-y-2 px-(--panel-x) py-3 sm:grid-cols-[auto_minmax(0,1fr)_auto]"
    >
      <div className="flex items-center gap-3 self-stretch">
        {writer && <Checkbox checked={selected} onCheckedChange={(v) => onSelect(!!v)} aria-labelledby={titleId} />}
        <span
          className="w-1 self-stretch rounded-pill"
          style={{ background: TONE_VAR[alertTone(alert.kind)] }}
          aria-hidden="true"
        />
      </div>
      <div className="min-w-0">
        <p id={titleId} className="text-body text-foreground">
          <InboxTitle alert={alert} />
        </p>
        {age && <p className="mono text-mono text-mute">{t('common.ago', { value: age })}</p>}
      </div>
      <div className="col-start-2 flex flex-wrap items-center gap-2 sm:col-start-3 sm:justify-end">
        <FixAction alert={alert} nodeId={nodeId} writer={writer} />
        {writer && (
          <Button type="button" size="sm" variant="outline" disabled={reading} onClick={onRead}>
            {t('dashboard.inbox_read')}
          </Button>
        )}
      </div>
    </li>
  );
}
