import { ArrowLeft } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom';

import { useApplyNode, useDeleteNode, useInstallCommand, useNode, useRestartNode } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { CopyButton } from '@/components/common/CopyButton';
import { PageHeader } from '@/components/common/PageHeader';
import { StatusBadge } from '@/components/common/StatusBadge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';

import { InstallCommandDialog } from './InstallCommandDialog';
import { NodeLogs } from './NodeLogs';
import { NodeOverviewTab } from './NodeOverviewTab';
import { NodeProfilesTab } from './NodeProfilesTab';
import { NodeSiteTab } from './NodeSiteTab';
import { NodeStatsTab } from './NodeStatsTab';
import { DASH, fakeTlsEndpoint, nodeStatus, shortVersion, telemtVersion } from './nodeDisplay';

/** One machine fact about the node, as a hairline tag beside the hostname. */
function MetaTag({ label, value }: { label: string; value: string }) {
  return (
    <span className="mono inline-flex items-center gap-1.5 rounded-sm border border-hairline px-1.5 py-0.5 text-xs">
      <span className="text-dim">{label}</span>
      <span className="text-mute">{value}</span>
    </span>
  );
}

export function NodeDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { t } = useTranslation();
  const { isWriter } = useAuth();
  const navigate = useNavigate();

  const nodeQuery = useNode(id ?? '');
  const applyNode = useApplyNode(id ?? '');
  const restartNode = useRestartNode(id ?? '');
  const deleteNode = useDeleteNode();
  const installCommand = useInstallCommand(id ?? '');

  const [restartOpen, setRestartOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [installResult, setInstallResult] = useState<{ command: string; expires_at: string } | null>(null);

  if (!id) return <Navigate to="/nodes" replace />;

  if (nodeQuery.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (nodeQuery.isError || !nodeQuery.data) {
    return (
      <>
        <PageHeader title={t('nodes.title')} />
        <p className="rounded-lg border border-hairline px-3 py-6 text-center text-sm text-mute">
          {nodeQuery.error instanceof ApiError ? nodeQuery.error.message : t('common.error_generic')}
        </p>
      </>
    );
  }

  const node = nodeQuery.data;
  // The restart button acts on whatever proxy the node runs: tproxy-server on a
  // tproxy node, telemt on a telemt one. Naming the wrong daemon in a destructive
  // confirmation is how an operator restarts the wrong thing.
  const restartLabel = t(node.engine === 'telemt' ? 'nodes.detail_restart_telemt' : 'nodes.detail_restart_relay');

  const handleApply = async () => {
    try {
      await applyNode.mutateAsync();
      toast.add({ description: t('nodes.apply_queued'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleRestart = async () => {
    try {
      await restartNode.mutateAsync();
      toast.add({ description: t('nodes.restart_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleDelete = async () => {
    try {
      await deleteNode.mutateAsync(node.id);
      toast.add({ description: t('nodes.delete_success'), type: 'success' });
      navigate('/nodes', { replace: true });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleShowInstall = async () => {
    try {
      const result = await installCommand.mutateAsync();
      setInstallResult(result);
    } catch {
      toast.add({ description: t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <>
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-3">
          <Link to="/nodes" className="mono inline-flex w-fit items-center gap-1 text-xs text-dim hover:text-foreground">
            <ArrowLeft className="size-3" />
            {t('nodes.title')}
          </Link>

          <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div className="min-w-0">
              <div className="flex items-center gap-1.5">
                <h1 className="flex min-w-0 items-center gap-2.5 text-xl font-semibold tracking-[-0.015em] text-foreground">
                  <StatusBadge status={nodeStatus(node)} hideLabel />
                  <span className="truncate">{node.name}</span>
                </h1>
                <HelpButton topic="nodes.detail" />
              </div>
              <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1.5">
                <span className="mono inline-flex items-center gap-0.5 text-xs text-mute">
                  {node.hostname}
                  <CopyButton value={node.hostname} className="size-5 [&_svg]:size-3" />
                </span>
                {node.engine === 'telemt' ? (
                  <>
                    <MetaTag label="telemt" value={telemtVersion(node) || DASH} />
                    {fakeTlsEndpoint(node) && <MetaTag label="tls" value={fakeTlsEndpoint(node)} />}
                  </>
                ) : (
                  <MetaTag label="relay" value={shortVersion(node.tproxy_version)} />
                )}
                <MetaTag label="agent" value={node.agent_version || DASH} />
              </div>
            </div>

            {isWriter && (
              <div className="flex flex-wrap items-center gap-2 lg:justify-end">
                <Button type="button" size="sm" onClick={() => void handleApply()} disabled={applyNode.isPending}>
                  {t('nodes.detail_apply_now')}
                </Button>
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
                <Button type="button" variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>
                  {t('nodes.detail_delete')}
                </Button>
              </div>
            )}
          </div>
        </div>

        <Tabs defaultValue="overview">
          <TabsList variant="line" className="w-full justify-start overflow-x-auto">
            <TabsTrigger value="overview">{t('nodes.tab_overview')}</TabsTrigger>
            <TabsTrigger value="profiles">{t('nodes.tab_profiles')}</TabsTrigger>
            <TabsTrigger value="logs">{t('nodes.tab_logs')}</TabsTrigger>
            <TabsTrigger value="site">{t('nodes.tab_site')}</TabsTrigger>
            <TabsTrigger value="stats">{t('nodes.tab_stats')}</TabsTrigger>
          </TabsList>

          <TabsContent value="overview">
            <NodeOverviewTab node={node} />
          </TabsContent>
          <TabsContent value="profiles">
            <NodeProfilesTab nodeId={node.id} online={node.online} engine={node.engine} />
          </TabsContent>
          <TabsContent value="logs">
            <NodeLogs nodeId={node.id} online={node.online} />
          </TabsContent>
          <TabsContent value="site">
            <NodeSiteTab nodeId={node.id} />
          </TabsContent>
          <TabsContent value="stats">
            <NodeStatsTab nodeId={node.id} online={node.online} />
          </TabsContent>
        </Tabs>
      </div>

      <ConfirmDialog
        open={restartOpen}
        onOpenChange={setRestartOpen}
        title={t('nodes.restart_confirm_title', { name: node.name })}
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
          command={installResult.command}
          expiresAt={installResult.expires_at}
          regenerated
        />
      )}
    </>
  );
}
