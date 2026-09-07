import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useAssignSite, useNodeSite } from '@/api/nodes';
import { nodeSitePreviewUrl, useSiteTemplates } from '@/api/sites';
import { useAuth } from '@/auth/AuthProvider';
import { PanelEmpty } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { ENTER_CLASS } from '@/components/ui/motion';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

/**
 * The cover site: which template this node serves, whether the bundle the node
 * is actually running matches the one the panel holds, and what that bundle
 * contains - then the page itself, rendered.
 *
 * The hash comparison is the only thing here that can be wrong, so it is the
 * only thing that carries colour: a green word when the two hashes agree, an
 * amber one while the node is still serving the previous bundle.
 */
export function NodeSiteTab({ nodeId }: { nodeId: string }) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const siteQuery = useNodeSite(nodeId);
  const templatesQuery = useSiteTemplates();
  const assignSite = useAssignSite(nodeId);
  const [selected, setSelected] = useState<string>('');

  const site = siteQuery.data;
  const templates = templatesQuery.data?.items ?? [];
  const currentTemplate = templates.find((tpl) => tpl.id === site?.template_id);
  const deployed = !!site?.deployed_hash && site.deployed_hash === site.bundle_hash;

  const handleAssign = async () => {
    if (!selected) return;
    try {
      await assignSite.mutateAsync(selected);
      toast.add({ description: t('nodes.site_assign_success'), type: 'success' });
      setSelected('');
    } catch {
      toast.add({ description: t('common.error_generic'), type: 'error' });
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

  /*
   * "We could not ask" is a different fact from "no template assigned", and
   * without this branch the failed request renders the second one - which
   * would send a writer off to assign a template that is already there.
   */
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
          title={t('nodes.site_current_template')}
          meta={site?.updated_at ? formatDateTime(site.updated_at, i18n.language) : undefined}
          actions={<HelpButton topic="sites.assign" />}
        />

        <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
          <span className="text-body text-foreground">
            {site?.template_id ? (currentTemplate?.name ?? site.template_id) : t('nodes.site_none')}
          </span>
          {site?.template_id && (
            <>
              <span className="inline-flex items-center gap-1.5 text-micro">
                <span className={cn('size-[7px] shrink-0 rounded-pill', deployed ? 'bg-ok' : 'bg-warn')} aria-hidden="true" />
                <span className={deployed ? 'text-ok' : 'text-warn'}>
                  {deployed ? t('nodes.site_status_deployed') : t('nodes.site_status_pending')}
                </span>
              </span>
              {site.bundle_hash && <span className="mono text-mono text-dim">{site.bundle_hash.slice(0, 12)}</span>}
            </>
          )}

          {isWriter && (
            <div className="flex w-full items-center gap-2 sm:ml-auto sm:w-auto">
              <Select value={selected} onValueChange={(v) => setSelected(v ?? '')}>
                <SelectTrigger size="sm" className="min-w-44">
                  {/* Resolve the label explicitly - SelectValue would otherwise show the raw
                      template id (a UUID) until the popup has mounted at least once. */}
                  <SelectValue placeholder={t('nodes.site_assign_label')}>
                    {(v: string) => templates.find((tpl) => tpl.id === v)?.name ?? t('nodes.site_assign_label')}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {templates.map((tpl) => (
                    <SelectItem key={tpl.id} value={tpl.id}>
                      {tpl.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button type="button" size="sm" onClick={() => void handleAssign()} disabled={!selected || assignSite.isPending}>
                {t('nodes.site_assign_button')}
              </Button>
            </div>
          )}
        </div>

        {site?.files && site.files.length > 0 && (
          <ul className="flex flex-wrap gap-x-4 gap-y-1 border-t border-hairline px-4 py-3">
            {site.files.map((f) => (
              <li key={f} className="mono text-mono text-dim">
                {f}
              </li>
            ))}
          </ul>
        )}
      </Panel>

      <Panel>
        <PanelHeader title={t('nodes.site_preview')} />
        {site?.template_id ? (
          <iframe
            key={site.bundle_hash}
            sandbox=""
            src={nodeSitePreviewUrl(nodeId)}
            title={t('nodes.site_preview')}
            className="m-4 h-96 w-[calc(100%-2rem)] rounded-surface border border-hairline bg-white"
          />
        ) : (
          <PanelEmpty>{t('nodes.site_no_preview')}</PanelEmpty>
        )}
      </Panel>
    </div>
  );
}
