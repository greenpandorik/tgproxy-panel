import { Copy, LayoutTemplate, MoreHorizontal, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';

import { useCreateSiteTemplate, useDeleteSiteTemplate, useSiteTemplates } from '@/api/sites';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { EmptyState } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { PageHeader } from '@/components/common/PageHeader';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { api, ApiError } from '@/lib/api';
import { formatRelativeTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import type { CSSProperties } from 'react';
import type { SiteTemplate } from '@/api/types';

/** The grid the list lays its cards out on - shared by the cards, their skeletons and nothing else. */
const CARD_GRID = 'grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3';

/**
 * The mark that says what kind of thing this card is.
 *
 * A card is not a table row, so its identity is a glyph on a tinted plate
 * rather than a 7px dot - the same plate a stat tile draws, in the neutral
 * tone, because which template a card holds is a fact and not a state. It is
 * what lets the eye find the template it wants in a grid of nine cards whose
 * names are all one line of the same weight.
 */
function TemplatePlate() {
  return (
    <span
      className="tgwp-tone-tint flex size-7 shrink-0 items-center justify-center rounded-control border"
      style={{ '--tone': 'var(--mute)' } as CSSProperties}
      aria-hidden="true"
    >
      <LayoutTemplate size={16} strokeWidth={1.8} />
    </span>
  );
}

/** The card in silhouette: the plate and name line, then the badge and stamp that sit at its foot. */
function TemplateCardSkeleton() {
  return (
    <li className="flex min-h-24 flex-col gap-2 rounded-surface border border-hairline bg-card p-4">
      <div className="flex items-center gap-2.5">
        <Skeleton className="size-7 rounded-control" />
        <Skeleton className="h-4 w-40" />
      </div>
      <div className="mt-auto flex items-center gap-2">
        <Skeleton className="h-5 w-14" />
        <Skeleton className="h-3 w-28" />
      </div>
    </li>
  );
}

export function SiteTemplatesPage() {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const navigate = useNavigate();
  const templatesQuery = useSiteTemplates();
  const createTemplate = useCreateSiteTemplate();
  const deleteTemplate = useDeleteSiteTemplate();

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
        <ul className={CARD_GRID}>
          {Array.from({ length: 3 }).map((_, i) => (
            <TemplateCardSkeleton key={i} />
          ))}
        </ul>
      ) : templatesQuery.isError ? (
        /*
         * The list failed rather than came back empty, so it says so where the
         * cards would have been and offers the one useful move: ask again.
         */
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
        <ul className={CARD_GRID}>
          {templates.map((tpl, i) => (
            /*
             * A template is a file, so its card is the panel the rest of the
             * app uses for a box of machine facts: hairline, no shadow, name in
             * the sans face and everything the machine wrote (preset origin,
             * last change) in mono underneath.
             */
            <li
              key={tpl.id}
              style={enterDelay(i)}
              className={cn(
                ENTER_CLASS,
                'flex min-h-24 flex-col gap-2 rounded-surface border border-hairline bg-card p-4 transition-colors hover:border-hairline-strong',
              )}
            >
              <div className="flex items-start justify-between gap-2">
                <span className="flex min-w-0 items-center gap-2.5">
                  <TemplatePlate />
                  <Link
                    to={`/sites/${tpl.id}`}
                    className="min-w-0 truncate text-body font-medium text-foreground hover:underline"
                  >
                    {tpl.name}
                  </Link>
                </span>
                {isWriter && (
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={<Button type="button" variant="ghost" size="icon-sm" className="-mt-0.5 -mr-1" />}
                    >
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
                {tpl.is_preset && <Badge>{t('sites.preset_badge')}</Badge>}
                <span className="mono text-mono text-mute">
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
