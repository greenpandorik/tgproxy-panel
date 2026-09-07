import { zodResolver } from '@hookform/resolvers/zod';
import { Lock, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { useActivateBranding, useBrandingProfiles, useCreateBrandingProfile, useDeleteBrandingProfile } from '@/api/branding';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { EmptyState } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { formatRelativeTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import { BrandingForm } from './BrandingForm';
import { Arriving, FormFooter } from './formShell';

import type { BrandingProfile } from '@/api/types';

const createSchema = z.object({ name: z.string().trim().min(1) });
type CreateFormValues = z.infer<typeof createSchema>;

/** Dialog to create a new profile as a copy of the currently active one. */
function CreateProfileDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (profile: BrandingProfile) => void;
}) {
  const { t } = useTranslation();
  const createProfile = useCreateBrandingProfile();
  const {
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<CreateFormValues>({ resolver: zodResolver(createSchema), defaultValues: { name: '' } });

  const onSubmit = async (values: CreateFormValues) => {
    try {
      const created = await createProfile.mutateAsync(values.name);
      reset();
      onOpenChange(false);
      onCreated(created);
      toast.add({ description: t('settings.branding_create_success'), type: 'success' });
    } catch (err) {
      if (err instanceof ApiError && err.fields.name) {
        setError('name', { message: err.fields.name });
        return;
      }
      setError('root', { message: err instanceof ApiError ? err.message : t('common.error_generic') });
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset();
        onOpenChange(next);
      }}
    >
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <div className="flex items-center gap-1.5">
            <DialogTitle>{t('settings.branding_create_dialog_title')}</DialogTitle>
            <HelpButton topic="settings.branding" className="-my-1.5" />
          </div>
          <DialogDescription>{t('settings.branding_create_dialog_description')}</DialogDescription>
        </DialogHeader>

        <form className="space-y-4" onSubmit={(e) => void handleSubmit(onSubmit)(e)} noValidate>
          <div className="space-y-2">
            <Label htmlFor="branding-profile-name">{t('settings.branding_create_field_name')}</Label>
            <Input
              id="branding-profile-name"
              autoFocus
              {...register('name')}
              aria-invalid={!!errors.name}
              aria-describedby={errors.name ? 'branding-profile-name-error' : undefined}
            />
            {errors.name && (
              <p id="branding-profile-name-error" className="text-label text-destructive">
                {t('common.required')}
              </p>
            )}
          </div>

          {errors.root && (
            <p
              role="alert"
              className="rounded-control border border-destructive/30 bg-destructive/10 px-3 py-2 text-body text-destructive"
            >
              {errors.root.message}
            </p>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={isSubmitting}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={isSubmitting}>
              {t('settings.branding_create_submit')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/**
 * One profile in the rail: the name, a dot and tag when it is the one the
 * panel is actually serving, and the mono stamp of its last change. The
 * selected row carries the same brand-coloured 2px tab the sidebar uses for
 * the current section - it is the same idea (you are here), so it is the same
 * mark. Activate/Delete stay out of the way until the row is pointed at.
 */
function ProfileRow({
  profile,
  selected,
  onSelect,
  onActivate,
  onDelete,
}: {
  profile: BrandingProfile;
  selected: boolean;
  onSelect: () => void;
  onActivate: () => void;
  onDelete: () => void;
}) {
  const { t, i18n } = useTranslation();

  return (
    <li
      className={cn(
        'group/profile relative flex items-center gap-2 px-4 py-3 transition-[background-color,scale] active:scale-[0.985]',
        selected
          ? 'bg-elevated before:absolute before:top-2 before:bottom-2 before:left-0 before:w-0.5 before:rounded-pill before:bg-brand-primary before:content-[""]'
          : 'hover:bg-elevated/50',
      )}
    >
      <button type="button" className="min-w-0 flex-1 text-left" aria-pressed={selected} onClick={onSelect}>
        <span className="flex items-center gap-2">
          {profile.is_active && <span className="size-[7px] shrink-0 rounded-pill bg-ok" aria-hidden="true" />}
          <span className="truncate text-body font-medium text-foreground">{profile.name}</span>
          {profile.is_active && <Badge className="shrink-0">{t('settings.branding_active_badge')}</Badge>}
        </span>
        <span className="mono mt-0.5 block truncate text-mono text-dim">
          {t('settings.branding_updated', { time: formatRelativeTime(profile.updated_at, i18n.language) })}
        </span>
      </button>
      {!profile.is_active && (
        <span className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover/profile:opacity-100 focus-within:opacity-100">
          <Button type="button" variant="ghost" size="xs" onClick={onActivate}>
            {t('settings.branding_activate')}
          </Button>
          <Button type="button" variant="ghost" size="icon-xs" onClick={onDelete} aria-label={t('common.delete')}>
            <Trash2 />
          </Button>
        </span>
      )}
    </li>
  );
}

/** Branding tab: a list of branding profiles on the left, the editor for the selected one
 * on the right. Below 1024px the columns stack. Writers only - the list endpoint itself
 * is writers-only, so viewers get a short no-access state instead. */
export function BrandingProfilesList() {
  const { t } = useTranslation();
  const { isWriter } = useAuth();
  const profilesQuery = useBrandingProfiles({ enabled: isWriter });
  const activateProfile = useActivateBranding();
  const deleteProfile = useDeleteBrandingProfile();

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);
  const [pendingSelectId, setPendingSelectId] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [activateTarget, setActivateTarget] = useState<BrandingProfile | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<BrandingProfile | null>(null);

  const items = profilesQuery.data?.items ?? [];
  const active = items.find((p) => p.is_active);
  const selected = items.find((p) => p.id === selectedId) ?? active;

  if (!isWriter) {
    return (
      <EmptyState
        icon={Lock}
        title={t('settings.branding_no_access_title')}
        description={t('settings.branding_no_access_description')}
      />
    );
  }

  const trySelect = (id: string) => {
    if (id === selected?.id) return;
    if (dirty) {
      setPendingSelectId(id);
    } else {
      setSelectedId(id);
    }
  };

  const handleActivate = async () => {
    if (!activateTarget) return;
    try {
      await activateProfile.mutateAsync(activateTarget.id);
      toast.add({ description: t('settings.branding_activate_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await deleteProfile.mutateAsync(deleteTarget.id);
      toast.add({ description: t('settings.branding_delete_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  if (profilesQuery.isLoading) {
    // The rail on the left and the stack of form panels on the right, in silhouette.
    return (
      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-[264px_1fr]">
        <Skeleton className="h-64 w-full rounded-surface" />
        <div className="space-y-4">
          <Skeleton className="h-48 w-full rounded-surface" />
          <Skeleton className="h-40 w-full rounded-surface" />
          <Skeleton className="h-14 w-full rounded-surface" />
        </div>
      </div>
    );
  }

  if (profilesQuery.isError || !selected) {
    return (
      <ErrorState
        message={profilesQuery.error instanceof ApiError ? profilesQuery.error.message : t('common.error_generic')}
        retryLabel={t('common.refresh')}
        onRetry={() => void profilesQuery.refetch()}
      />
    );
  }

  return (
    <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-[264px_1fr]">
      {/* Head, body, footer, like every other form behind these tabs: the rail's
          own action sits in the footer rather than in a 264px header that cannot
          hold both a title and this label. */}
      <div className="flex flex-col gap-4">
        <Arriving>
          <Panel>
            <PanelHeader
              title={t('settings.branding_profiles_title')}
              meta={String(items.length)}
              actions={<HelpButton topic="settings.branding" />}
            />
            <ul className="divide-y divide-hairline" aria-label={t('settings.branding_profiles_title')}>
              {items.map((p) => (
                <ProfileRow
                  key={p.id}
                  profile={p}
                  selected={p.id === selected.id}
                  onSelect={() => trySelect(p.id)}
                  onActivate={() => setActivateTarget(p)}
                  onDelete={() => setDeleteTarget(p)}
                />
              ))}
            </ul>
          </Panel>
        </Arriving>

        <FormFooter>
          <Button type="button" variant="outline" onClick={() => setCreateOpen(true)}>
            <Plus />
            {t('settings.branding_create_from_active')}
          </Button>
        </FormFooter>
      </div>

      <div className="min-w-0">
        <BrandingForm key={selected.id} profile={selected} onDirtyChange={setDirty} />
      </div>

      <CreateProfileDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={(p) => trySelect(p.id)} />

      <ConfirmDialog
        open={!!activateTarget}
        onOpenChange={(open) => !open && setActivateTarget(null)}
        title={t('settings.branding_activate_confirm_title', { name: activateTarget?.name ?? '' })}
        description={t('settings.branding_activate_confirm_description')}
        confirmLabel={t('settings.branding_activate')}
        onConfirm={handleActivate}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('settings.branding_delete_confirm_title', { name: deleteTarget?.name ?? '' })}
        description={t('settings.branding_delete_confirm_description')}
        destructive
        confirmLabel={t('common.delete')}
        onConfirm={handleDelete}
      />

      <ConfirmDialog
        open={pendingSelectId !== null}
        onOpenChange={(open) => !open && setPendingSelectId(null)}
        title={t('settings.branding_discard_confirm_title')}
        description={t('settings.branding_discard_confirm_description')}
        destructive
        confirmLabel={t('settings.branding_discard_confirm_action')}
        onConfirm={() => {
          if (pendingSelectId) setSelectedId(pendingSelectId);
          setPendingSelectId(null);
        }}
      />
    </div>
  );
}
