import { Eye, LayoutTemplate, Network } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useAssignSite, useAssignUpstreamSite, useNodeSite, useNode, useNodeJobs } from '@/api/nodes';
import { nodeSitePreviewUrl, useSiteTemplates } from '@/api/sites';
import { useAuth } from '@/auth/AuthProvider';
import { PanelEmpty } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ENTER_CLASS } from '@/components/ui/motion';
import { WebsiteGallery } from '@/pages/sites/WebsiteGallery';
import { WebsitePreview } from '@/pages/sites/WebsitePreview';
import type { SiteTemplate } from '@/api/types';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

export function NodeSiteTab({ nodeId }: { nodeId: string }) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const siteQuery = useNodeSite(nodeId);
  const templatesQuery = useSiteTemplates();
  const assignSite = useAssignSite(nodeId);
  const assignUpstream = useAssignUpstreamSite(nodeId);
  const [picker, setPicker] = useState(false);
  const [upstreamOpen, setUpstreamOpen] = useState(false);
  const [origin, setOrigin] = useState('http://127.0.0.1:3000');
  const [preview, setPreview] = useState<SiteTemplate | null>(null);
  const nodeQuery = useNode(nodeId);
  const jobsQuery = useNodeJobs(nodeId, 1);

  const site = siteQuery.data;
  const templates = templatesQuery.data?.items ?? [];
  const currentTemplate = templates.find((tpl) => tpl.id === site?.template_id);
  const deployed = !!site?.deployed_hash && site.deployed_hash === site.bundle_hash;
  const isUpstream = site?.mode === 'upstream';
  const upstreamSupported = nodeQuery.data?.engine === 'telemt' && nodeQuery.data.telemt_capabilities?.HttpUpstreamDecoy === true;

  const handleAssign = async (selected: SiteTemplate) => {
    try {
      await assignSite.mutateAsync(selected.id);
      toast.add({ description: t('nodes.site_assign_success'), type: 'success' });
      setPicker(false);
    } catch {
      toast.add({ description: t('common.error_generic'), type: 'error' });
    }
  };

  const handleAssignUpstream = async () => {
    try {
      await assignUpstream.mutateAsync(origin);
      toast.add({ description: t('sites.upstream_queued'), type: 'success' });
      setPicker(false);
      setUpstreamOpen(false);
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  if (siteQuery.isLoading) {
    return (
      <Panel>
        <div className="space-y-4 p-4">
          <Skeleton className="h-4 w-40" />
          <Skeleton className="h-64 w-full" />
        </div>
      </Panel>
    );
  }

  if (siteQuery.isError) {
    return (
      <Panel>
        <ErrorState
          inset
          message={siteQuery.error instanceof ApiError ? siteQuery.error.message : t('common.error_generic')}
          retryLabel={t('common.refresh')}
          onRetry={() => void siteQuery.refetch()}
        />
      </Panel>
    );
  }

  return (
    <div className={cn(ENTER_CLASS, 'flex flex-col gap-4')}>
      <Panel>
        <PanelHeader
          icon={LayoutTemplate}
          title={t('nodes.site_current_template')}
          meta={site?.updated_at ? formatDateTime(site.updated_at, i18n.language) : undefined}
          actions={<HelpButton topic="sites.assign" />}
        />

        <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
          <span className="text-body text-foreground">
            {isUpstream ? t('sites.upstream_title') : currentTemplate?.display_name ?? currentTemplate?.name ?? (site?.bundle_hash ? t('sites.legacy') : t('nodes.site_none'))}
          </span>
          {site?.bundle_hash && (
            <>
              <span className="inline-flex items-center gap-1.5 text-micro">
                <span className={cn('size-[7px] shrink-0 rounded-pill', deployed ? 'bg-ok' : 'bg-warn')} aria-hidden="true" />
                <span className={deployed ? 'text-ok' : 'text-warn'}>
                  {deployed ? t('nodes.site_status_deployed') : t('nodes.site_status_pending')}
                </span>
              </span>
              {site.bundle_hash && <span className="mono text-mono text-mute">{site.bundle_hash.slice(0, 12)}</span>}
            </>
          )}

          {isWriter && (
            <div className="flex flex-wrap gap-2 sm:ml-auto">
              <Button variant="outline" onClick={() => { setPicker(!picker); setUpstreamOpen(false); }} disabled={assignSite.isPending}>{t('sites.change')}</Button>
              {nodeQuery.data?.engine === 'telemt' && <Button variant="outline" onClick={() => { setUpstreamOpen(!upstreamOpen); setPicker(false); }} disabled={!upstreamSupported || assignUpstream.isPending}><Network />{t('sites.upstream_action')}</Button>}
            </div>
          )}
        </div>

        {nodeQuery.data && <div className="border-t border-hairline px-4 py-3"><a className="text-brand-ink underline underline-offset-4" href={`https://${nodeQuery.data.hostname}`} target="_blank" rel="noreferrer">https://{nodeQuery.data.hostname}</a></div>}
        {isUpstream && site.origin && <div className="border-t border-hairline px-4 py-3"><span className="text-label text-mute">{t('sites.upstream_origin')} </span><span className="mono text-mono">{site.origin}</span></div>}
        {!deployed && site?.bundle_hash && <p className="px-4 pb-4 text-label text-mute" role="status">{t('sites.deploy_pending')}</p>}
        {jobsQuery.data?.items[0]?.status === 'failed' && !deployed && <p className="px-4 pb-4 text-destructive" role="alert">{t('sites.deploy_failed')}</p>}
        <details className="border-t border-hairline p-4"><summary className="cursor-pointer text-label text-mute">{t('sites.files')}</summary><ul className="mt-2 text-mono text-mute">{site?.files?.map((f) => <li key={f}>{f}</li>)}</ul></details>
      </Panel>

      {upstreamOpen && <Panel>
        <PanelHeader icon={Network} title={t('sites.upstream_title')} />
        <form className="space-y-4 p-4" onSubmit={(event) => { event.preventDefault(); void handleAssignUpstream(); }}>
          <p className="max-w-3xl text-label text-mute">{t('sites.upstream_hint')}</p>
          <div className="max-w-xl space-y-2">
            <Label htmlFor="site-upstream-origin">{t('sites.upstream_origin')}</Label>
            <Input id="site-upstream-origin" className="mono" type="url" required pattern="http://.*" value={origin} onChange={(event) => setOrigin(event.target.value)} aria-describedby="site-upstream-security" />
            <p id="site-upstream-security" className="text-micro text-mute">{t(upstreamSupported ? 'sites.upstream_security' : 'sites.upstream_unsupported')}</p>
          </div>
          {assignUpstream.isError && <p role="alert" className="text-destructive">{assignUpstream.error.message}</p>}
          <div className="flex flex-wrap gap-2"><Button type="submit" disabled={!upstreamSupported || assignUpstream.isPending}>{t('sites.upstream_test_apply')}</Button><Button type="button" variant="ghost" onClick={() => setUpstreamOpen(false)}>{t('common.cancel')}</Button></div>
        </form>
      </Panel>}

      {picker && <div className="space-y-4">
        {templatesQuery.isError ? <ErrorState message={templatesQuery.error.message} onRetry={() => void templatesQuery.refetch()} retryLabel={t('common.refresh')} /> : <WebsiteGallery websites={templates} currentId={site?.template_id} onPreview={setPreview} onUse={assignSite.isPending ? undefined : (tpl) => void handleAssign(tpl)} />}
      </div>}
      <WebsitePreview website={preview} onClose={() => setPreview(null)} onUse={isWriter ? (tpl) => { setPreview(null); void handleAssign(tpl); } : undefined} />
      <Panel>
        <PanelHeader icon={Eye} title={t('nodes.site_preview')} />
        {site?.bundle_hash && !isUpstream ? (
          <iframe
            key={site.bundle_hash}
            sandbox=""
            src={nodeSitePreviewUrl(nodeId)}
            title={t('nodes.site_preview')}
            className="m-4 h-96 w-[calc(100%-2rem)] rounded-surface border border-hairline bg-white"
          />
        ) : (
          <PanelEmpty>{isUpstream ? t('sites.upstream_preview_hint') : t('nodes.site_no_preview')}</PanelEmpty>
        )}
      </Panel>
    </div>
  );
}
