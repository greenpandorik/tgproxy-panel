import { RefreshCw, Server } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Link, Navigate, useParams, useSearchParams } from 'react-router-dom';

import { useApplyNode, useNode } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { BackButton } from '@/components/common/BackButton';
import { CopyButton } from '@/components/common/CopyButton';
import { EmptyState } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { SectionTabs, useSection } from '@/components/common/SectionNav';
import { StatusBadge } from '@/components/common/StatusBadge';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';

import { NodeBlocklist } from './NodeBlocklist';
import { NodeMaintenanceTab } from './NodeMaintenanceTab';
import { NodeOverviewTab } from './NodeOverviewTab';
import { NodeProxyTab } from './NodeProxyTab';
import { NodeSiteTab } from './NodeSiteTab';
import { DASH, nodeStatus, shortVersion, telemtVersion } from './nodeDisplay';

import type { Node } from '@/api/types';

/** Old section names, from links made before their tabs moved into Состояние. */
const OLD_SECTIONS = { stats: 'overview', web: 'overview', diagnostics: 'overview', logs: 'overview', profiles: 'overview' };

/** One machine fact about the node, as a hairline tag beside the hostname. */
function MetaTag({ label, value }: { label: string; value: string }) {
  return (
    <Badge>
      <span className="text-mute">{label}</span>
      <span className="text-foreground">{value}</span>
    </Badge>
  );
}

/** The Fake-TLS listener as a tag: just the port when it masks behind the server's own hostname. */
function tlsTag(node: Node): string {
  if (node.engine !== 'telemt' || !node.tls_domain) return '';
  return node.tls_domain === node.hostname ? `:${node.classic_port}` : `${node.tls_domain}:${node.classic_port}`;
}

function BackLink() {
  const { t } = useTranslation();
  return <BackButton to="/nodes" label={t('nodes.back_all')} />;
}

function ApplyButton({ node }: { node: Node }) {
  const { t } = useTranslation();
  const applyNode = useApplyNode(node.id);
  const handleApply = async () => {
    try {
      await applyNode.mutateAsync();
      toast.add({ description: t('nodes.apply_queued'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex" />}>
        <Button type="button" onClick={() => void handleApply()} disabled={applyNode.isPending || !node.dirty}>
          <RefreshCw className={applyNode.isPending ? 'animate-spin' : undefined} />
          {t('nodes.detail_apply_now')}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{t(node.dirty ? 'nodes.detail_apply_hint' : 'nodes.detail_apply_up_to_date')}</TooltipContent>
    </Tooltip>
  );
}

export function NodeDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { t } = useTranslation();
  const { isWriter } = useAuth();
  const [params] = useSearchParams();
  const nodeQuery = useNode(id ?? '');
  const telemt = nodeQuery.data?.engine === 'telemt';
  const [section, setSection] = useSection(
    telemt ? ['overview', 'proxy', 'site', 'blocklist', 'settings'] : ['overview', 'site', 'blocklist', 'settings'],
    'overview',
    'section',
    OLD_SECTIONS,
  );
  const asked = params.get('section');
  const focus = asked === 'diagnostics' ? 'checks' : asked === 'web' ? 'web' : null;

  if (!id) return <Navigate to="/nodes" replace />;

  if (nodeQuery.isLoading) {
    return (
      <div className="space-y-6">
        <div className="space-y-3">
          <BackLink />
          <Skeleton className="h-8 w-64" />
          <Skeleton className="h-4 w-80 max-w-full" />
        </div>
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (nodeQuery.isError || !nodeQuery.data) {
    const missing = nodeQuery.error instanceof ApiError && nodeQuery.error.status === 404;
    return (
      <div className="flex flex-col gap-6">
        <BackLink />
        {missing ? (
          <EmptyState
            icon={Server}
            title={t('nodes.detail_not_found_title')}
            description={t('nodes.detail_not_found_description')}
            action={
              <Button variant="outline" nativeButton={false} render={<Link to="/nodes" />}>
                {t('nodes.detail_back_to_list')}
              </Button>
            }
          />
        ) : (
          <ErrorState
            message={t('common.error_generic')}
            retryLabel={t('common.refresh')}
            onRetry={() => void nodeQuery.refetch()}
          />
        )}
      </div>
    );
  }

  const node = nodeQuery.data;
  const tls = tlsTag(node);
  const tabs = [
    { value: 'overview', label: t('workspace.tab_overview') },
    ...(telemt ? [{ value: 'proxy', label: t('workspace.tab_proxy') }] : []),
    { value: 'site', label: t('workspace.tab_site') },
    { value: 'blocklist', label: t('workspace.tab_blocklist') },
    { value: 'settings', label: t('workspace.tab_settings') },
  ];
  const current = tabs.find((tab) => tab.value === section);

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-4">
        <BackLink />
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0">
            <div className="flex items-center gap-1.5">
              <h1 className="flex min-w-0 items-center gap-2.5 text-display text-foreground">
                <StatusBadge status={nodeStatus(node)} hideLabel />
                <span className="truncate">{node.name}</span>
              </h1>
              <HelpButton topic="nodes.detail" />
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1.5">
              <span className="mono inline-flex min-w-0 items-center gap-0.5 text-mono text-mute">
                <span className="truncate">{node.hostname}</span>
                <CopyButton value={node.hostname} className="size-5 [&_svg]:size-3" />
              </span>
              {telemt ? (
                <>
                  <MetaTag label="telemt" value={telemtVersion(node) || DASH} />
                  {tls && <MetaTag label="tls" value={tls} />}
                </>
              ) : (
                <MetaTag label="relay" value={shortVersion(node.tproxy_version)} />
              )}
              <MetaTag label={t('nodes.detail_tag_agent')} value={node.agent_version || DASH} />
            </div>
          </div>
          {isWriter && <ApplyButton node={node} />}
        </div>
      </div>

      <SectionTabs label={t('workspace.node_navigation')} value={section} onChange={setSection} items={tabs} />
      <section className="min-w-0" aria-label={current?.label}>
        {section === 'overview' && <NodeOverviewTab node={node} focus={focus} />}
        {section === 'proxy' && telemt && <NodeProxyTab node={node} canEdit={isWriter} />}
        {section === 'site' && <NodeSiteTab nodeId={node.id} />}
        {section === 'blocklist' && <NodeBlocklist node={node} />}
        {section === 'settings' && <NodeMaintenanceTab node={node} />}
      </section>
    </div>
  );
}
