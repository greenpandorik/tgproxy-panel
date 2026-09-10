import { Copy, LayoutTemplate, MoreHorizontal, Plus, Trash2, Upload } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';

import { useCreateSiteTemplate, useDeleteSiteTemplate, useSiteTemplates } from '@/api/sites';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { EmptyState } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { PageHeader } from '@/components/common/PageHeader';
import { Button } from '@/components/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { api, ApiError } from '@/lib/api';

import { CustomizeWebsiteDialog } from './CustomizeWebsiteDialog';
import { WebsiteGallery } from './WebsiteGallery';
import { WebsitePreview } from './WebsitePreview';
import { AssignTemplateDialog } from './AssignTemplateDialog';
import { ImportWebsiteDialog } from './ImportWebsiteDialog';
import type { SiteTemplate } from '@/api/types';

export function SiteTemplatesPage() {
  const { t } = useTranslation();
  const { isWriter } = useAuth();
  const navigate = useNavigate();
  const templatesQuery = useSiteTemplates();
  const createTemplate = useCreateSiteTemplate();
  const deleteTemplate = useDeleteSiteTemplate();

  const [customize, setCustomize] = useState<SiteTemplate | null>(null);
  const [preview, setPreview] = useState<SiteTemplate | null>(null);
  const [assign, setAssign] = useState<SiteTemplate | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<SiteTemplate | null>(null);

  const templates = templatesQuery.data?.items ?? [];

  const handleDuplicate = async (tpl: SiteTemplate) => {
    try {
      // The list endpoint doesn't include html/assets - fetch the full record first.
      const full = await api.get<SiteTemplate>(`/api/v1/site-templates/${tpl.id}`);
      const created = await createTemplate.mutateAsync({
        name: t('sites.duplicate_name', { name: full.name }),
        html: full.html ?? '',
        assets: full.assets ?? {},
      });
      toast.add({ description: t('sites.duplicate_success'), type: 'success' });
      navigate(`/sites/${created.id}`);
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await deleteTemplate.mutateAsync(deleteTarget.id);
      toast.add({ description: t('sites.delete_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <>
      <PageHeader
        title={t('sites.title')}
        description={templates.length > 0 ? t('sites.header_count', { count: templates.length }) : undefined}
        actions={
          <>
            <HelpButton topic="sites.templates" />
            {isWriter && <Button variant="outline" onClick={() => setImportOpen(true)}><Upload />{t('sites.import_zip')}</Button>}
            {isWriter && (
              <Button type="button" onClick={() => navigate('/sites/new')}>
                <Plus />
                {t('sites.create')}
              </Button>
            )}
          </>
        }
      />

      {templatesQuery.isLoading ? (
        <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 xl:grid-cols-3">{Array.from({length: 6}, (_, i) => <Skeleton key={i} className="h-80" />)}</div>
      ) : templatesQuery.isError ? (
        <ErrorState
          message={templatesQuery.error instanceof ApiError ? templatesQuery.error.message : t('common.error_generic')}
          retryLabel={t('common.refresh')}
          onRetry={() => void templatesQuery.refetch()}
        />
      ) : templates.length === 0 ? (
        <EmptyState
          icon={LayoutTemplate}
          title={t('sites.empty_title')}
          description={t('sites.empty_description')}
          action={
            isWriter && (
              <Button type="button" onClick={() => navigate('/sites/new')}>
                <Plus />
                {t('sites.create')}
              </Button>
            )
          }
        />
      ) : (
        <WebsiteGallery websites={templates} onPreview={setPreview} onUse={isWriter ? setAssign : undefined} actions={isWriter ? (tpl) => <DropdownMenu>
          <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" aria-label={t('common.actions')} />}><MoreHorizontal /></DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {tpl.is_preset && <DropdownMenuItem onClick={() => setCustomize(tpl)}>{t('sites.customize')}</DropdownMenuItem>}
            <DropdownMenuItem onClick={() => navigate(`/sites/${tpl.id}`)}>{t('common.edit')}</DropdownMenuItem>
            <DropdownMenuItem disabled={createTemplate.isPending} onClick={() => void handleDuplicate(tpl)}><Copy />{t('sites.action_duplicate')}</DropdownMenuItem>
            {!tpl.is_preset && <DropdownMenuItem variant="destructive" onClick={() => setDeleteTarget(tpl)}><Trash2 />{t('common.delete')}</DropdownMenuItem>}
          </DropdownMenuContent>
        </DropdownMenu> : undefined} />
      )}
      <WebsitePreview website={preview} onClose={() => setPreview(null)} onUse={isWriter ? (tpl) => { setPreview(null); setAssign(tpl); } : undefined} />
      {assign && <AssignTemplateDialog open onOpenChange={(open) => { if (!open) setAssign(null); }} templateId={assign.id} templateName={assign.display_name ?? assign.name} />}
      {customize && <CustomizeWebsiteDialog key={customize.id} website={customize} onClose={() => setCustomize(null)} />}
      <ImportWebsiteDialog open={importOpen} onOpenChange={setImportOpen} />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('sites.delete_confirm_title', { name: deleteTarget?.name ?? '' })}
        description={t('sites.delete_confirm_description')}
        destructive
        confirmLabel={t('common.delete')}
        onConfirm={handleDelete}
      />
    </>
  );
}
