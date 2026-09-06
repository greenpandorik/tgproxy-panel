import { Copy, MoreHorizontal, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';

import { useCreateSiteTemplate, useDeleteSiteTemplate, useSiteTemplates } from '@/api/sites';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { EmptyState } from '@/components/common/EmptyState';
import { PageHeader } from '@/components/common/PageHeader';
import { Button } from '@/components/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { api, ApiError } from '@/lib/api';
import { formatRelativeTime } from '@/lib/format';

import type { SiteTemplate } from '@/api/types';

export function SiteTemplatesPage() {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const navigate = useNavigate();
  const { data, isLoading } = useSiteTemplates();
  const createTemplate = useCreateSiteTemplate();
  const deleteTemplate = useDeleteSiteTemplate();

  const [deleteTarget, setDeleteTarget] = useState<SiteTemplate | null>(null);

  const templates = data?.items ?? [];

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
          isWriter && (
            <Button type="button" onClick={() => navigate('/sites/new')}>
              <Plus />
              {t('sites.create')}
            </Button>
          )
        }
      />

      {isLoading ? (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-[92px] w-full" />
          ))}
        </div>
      ) : templates.length === 0 ? (
        <EmptyState
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
        <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {templates.map((tpl) => (
            /*
             * A template is a file, so its card is the panel the rest of the
             * app uses for a box of machine facts: hairline, no shadow, name in
             * the sans face and everything the machine wrote (preset origin,
             * last change) in mono underneath.
             */
            <li
              key={tpl.id}
              className="flex min-h-[92px] flex-col gap-2 rounded-lg border border-hairline bg-card p-3.5 transition-colors hover:border-hairline-strong"
            >
              <div className="flex items-start justify-between gap-2">
                <Link to={`/sites/${tpl.id}`} className="min-w-0 truncate text-sm font-medium text-foreground hover:underline">
                  {tpl.name}
                </Link>
                {isWriter && (
                  <DropdownMenu>
                    <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="icon-sm" className="-mt-0.5 -mr-1" />}>
                      <MoreHorizontal />
                      <span className="sr-only">{t('common.actions')}</span>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem onClick={() => navigate(`/sites/${tpl.id}`)}>{t('common.edit')}</DropdownMenuItem>
                      <DropdownMenuItem onClick={() => void handleDuplicate(tpl)}>
                        <Copy />
                        {t('sites.action_duplicate')}
                      </DropdownMenuItem>
                      {!tpl.is_preset && (
                        <DropdownMenuItem variant="destructive" onClick={() => setDeleteTarget(tpl)}>
                          <Trash2 />
                          {t('common.delete')}
                        </DropdownMenuItem>
                      )}
                    </DropdownMenuContent>
                  </DropdownMenu>
                )}
              </div>

              <div className="mt-auto flex flex-wrap items-center gap-x-2 gap-y-1">
                {tpl.is_preset && (
                  <span className="mono rounded-sm border border-hairline-strong px-1.5 py-0.5 text-xs text-mute">
                    {t('sites.preset_badge')}
                  </span>
                )}
                <span className="mono text-xs text-dim">
                  {t('sites.card_updated', { time: formatRelativeTime(tpl.updated_at, i18n.language) })}
                </span>
              </div>
            </li>
          ))}
        </ul>
      )}

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
