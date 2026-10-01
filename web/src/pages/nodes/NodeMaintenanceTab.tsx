import { ExternalLink, Package, Wrench } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';

import { useDeleteNode, useInstallCommand, useNodeJobs, useRestartNode } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { CollapsibleSection } from '@/components/common/CollapsibleSection';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { TelemtUpdateCard } from '@/components/web/TelemtUpdateCard';
import { Button } from '@/components/ui/button';
import { ENTER_CLASS } from '@/components/ui/motion';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';
import { formatCompactAge } from '@/lib/format';
import { cn } from '@/lib/utils';

import { InstallCommandDialog } from './InstallCommandDialog';
import { NodeApplyHistory } from './NodeApplyHistory';
import { NodeProfilesSection } from './NodeProfilesSection';
import { DASH } from './nodeDisplay';
import { applyFailed } from './nodeHealth';

import type { Node } from '@/api/types';
import type { ReactNode } from 'react';

const TPROXY_REPO = 'https://github.com/telegramdesktop/tproxy-server';

function VersionRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-x-6 gap-y-2 px-(--panel-x) py-4 sm:grid-cols-[9rem_minmax(0,1fr)]">
      <span className="pt-0.5 text-label text-mute">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function TproxyVersion({ version }: { version: string }) {
  if (!version) return <span className="mono text-mono text-dim">{DASH}</span>;
  if (!/^[0-9a-f]{7,40}$/i.test(version)) return <span className="mono text-mono text-foreground">{version}</span>;
  return (
    <a
      href={`${TPROXY_REPO}/commit/${version}`}
      target="_blank"
      rel="noreferrer"
      className="mono inline-flex items-center gap-1 text-mono text-brand-ink underline-offset-3 hover:underline"
    >
      {version.slice(0, 12)}
      <ExternalLink className="size-3" aria-hidden="true" />
    </a>
  );
}

function History({ nodeId }: { nodeId: string }) {
  const { t, i18n } = useTranslation();
  const jobsQuery = useNodeJobs(nodeId);
  const jobs = jobsQuery.data?.items ?? [];
  const last = jobs[0];
  const failed = !!last && applyFailed(last.status);
  const age = last ? formatCompactAge(last.created_at, i18n.language) : null;
  const summary = last
    ? t('nodes.detail_history_last', {
        status: t(`nodes.job_status_${last.status}`, last.status),
        ago: age ? t('common.ago', { value: age }) : '',
      })
    : t('nodes.overview_jobs_empty');
  return (
    <Panel>
      <CollapsibleSection
        title={t('nodes.overview_jobs_title')}
        summary={jobsQuery.isLoading ? t('common.state.loading') : summary}
        tone={failed ? 'err' : 'neutral'}
      >
        <NodeApplyHistory
          jobs={jobs}
          loading={jobsQuery.isLoading}
          error={jobsQuery.isError}
          onRetry={() => void jobsQuery.refetch()}
        />
      </CollapsibleSection>
    </Panel>
  );
}

/** Upkeep of one server: what is installed, the few actions it needs, and removing it. */
export function NodeMaintenanceTab({ node }: { node: Node }) {
  const { t } = useTranslation();
  const { isWriter } = useAuth();
  const navigate = useNavigate();
  const restartNode = useRestartNode(node.id);
  const deleteNode = useDeleteNode();
  const installCommand = useInstallCommand(node.id);
  const [restartOpen, setRestartOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [installResult, setInstallResult] = useState<{ command: string; expires_at: string } | null>(null);
  const telemt = node.engine === 'telemt';
  const restartLabel = t(telemt ? 'nodes.detail_restart_telemt' : 'nodes.detail_restart_relay');
  const fail = (err: unknown) =>
    toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });

  const handleRestart = async () => {
    try {
      await restartNode.mutateAsync();
      toast.add({ description: t('nodes.restart_success'), type: 'success' });
    } catch (err) {
      fail(err);
    }
  };

  const handleDelete = async () => {
    try {
      await deleteNode.mutateAsync(node.id);
      toast.add({ description: t('nodes.delete_success'), type: 'success' });
      navigate('/nodes', { replace: true });
    } catch (err) {
      fail(err);
    }
  };

  const handleShowInstall = async () => {
    try {
      setInstallResult(await installCommand.mutateAsync());
    } catch (err) {
      fail(err);
    }
  };

  return (
    <div className={cn(ENTER_CLASS, 'flex flex-col gap-4')}>
      <Panel>
        <PanelHeader icon={Package} title={t('nodes.overview_versions')} />
        <div className="divide-y divide-hairline">
          <VersionRow label={telemt ? 'telemt' : t('nodes.overview_tproxy_version')}>
            {telemt ? <TelemtUpdateCard node={node} /> : <TproxyVersion version={node.tproxy_version} />}
          </VersionRow>
          <VersionRow label={t('nodes.overview_agent_version')}>
            <span className={cn('mono text-mono', node.agent_version ? 'text-foreground' : 'text-dim')}>
              {node.agent_version || DASH}
            </span>
          </VersionRow>
        </div>
      </Panel>

      <Panel>
        <PanelHeader icon={Wrench} title={t('nodes.detail_actions')} />
        <PanelBody className="space-y-4">
          {isWriter && (
            <>
              <div className="flex flex-wrap gap-3">
                <Button type="button" variant="outline" size="sm" onClick={() => setRestartOpen(true)}>
                  {restartLabel}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => void handleShowInstall()}
                  disabled={installCommand.isPending}
                >
                  {t('nodes.install_show')}
                </Button>
              </div>
              <p className="max-w-[72ch] text-label text-mute">{t('nodes.detail_actions_hint')}</p>
            </>
          )}
          <p className="max-w-[72ch] text-label text-mute">
            {t('nodes.detail_logs_docs')}{' '}
            <span className="mono text-mono text-foreground">{t('nodes.detail_logs_docs_file')}</span>
          </p>
        </PanelBody>
      </Panel>

      <History nodeId={node.id} />
      <NodeProfilesSection nodeId={node.id} online={node.online} engine={node.engine} />

      {isWriter && (
        <Panel className="border-destructive/40">
          <div className="flex flex-col gap-3 px-(--panel-x) py-4 sm:flex-row sm:items-center sm:justify-between sm:gap-6">
            <div className="min-w-0">
              <h2 className="text-title text-foreground">{t('nodes.detail_danger_title')}</h2>
              <p className="mt-1 max-w-[72ch] text-label text-mute">{t('nodes.detail_danger_hint')}</p>
            </div>
            <Button type="button" variant="destructive" size="sm" className="shrink-0" onClick={() => setDeleteOpen(true)}>
              {t('nodes.detail_delete')}
            </Button>
          </div>
        </Panel>
      )}

      <ConfirmDialog
        open={restartOpen}
        onOpenChange={setRestartOpen}
        title={t('nodes.restart_confirm_title_service', { name: node.name, service: telemt ? 'telemt' : 'relay' })}
        description={t('nodes.restart_confirm_description')}
        destructive
        confirmLabel={restartLabel}
        onConfirm={handleRestart}
      />
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t('nodes.delete_confirm_title', { name: node.name })}
        description={t('nodes.delete_confirm_description')}
        destructive
        confirmLabel={t('nodes.action_delete')}
        onConfirm={handleDelete}
      />
      {installResult && (
        <InstallCommandDialog
          open={!!installResult}
          onOpenChange={(open) => !open && setInstallResult(null)}
          nodeId={node.id}
          command={installResult.command}
          expiresAt={installResult.expires_at}
          regenerated
        />
      )}
    </div>
  );
}
