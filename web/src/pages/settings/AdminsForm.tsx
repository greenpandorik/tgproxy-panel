import { zodResolver } from '@hookform/resolvers/zod';
import { Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { useAdmins, useCreateAdmin, useDeleteAdmin } from '@/api/auth';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { DataTableSkeleton } from '@/components/common/DataTable';
import { DraftBanner } from '@/components/common/DraftBanner';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';
import { cn } from '@/lib/utils';
import { formatDate } from '@/lib/format';

import { Arriving, FormFooter } from './formShell';

import type { Admin, AdminRole } from '@/api/types';

const ROLE_BADGE_KEY: Record<AdminRole, string> = {
  owner: 'common.role_owner',
  admin: 'common.role_admin',
  viewer: 'common.role_viewer',
};

const schema = z.object({
  username: z.string().trim().min(2),
  password: z.string().min(10),
  role: z.enum(['owner', 'admin', 'viewer']),
});

type FormValues = z.infer<typeof schema>;

const EMPTY_VALUES: FormValues = { username: '', password: '', role: 'admin' };

/** What the create dialog remembers between openings - never the password. */
type AdminDraft = Pick<FormValues, 'username' | 'role'>;
const EMPTY_DRAFT: AdminDraft = { username: '', role: 'admin' };

/** Role as a mono tag: it is an enum the server owns, not a state that pulses. */
function RoleTag({ role }: { role: AdminRole }) {
  const { t } = useTranslation();
  return <Badge>{t(ROLE_BADGE_KEY[role])}</Badge>;
}

/** Creating an admin sets a password, so it happens in a dialog rather than in a form left standing open under the table. */
function CreateAdminDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useTranslation();
  const createAdmin = useCreateAdmin();

  const {
    register,
    handleSubmit,
    reset,
    setValue,
    watch,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: EMPTY_VALUES,
  });

  const role = watch('role');
  const username = watch('username');

  const draft = useDraft<AdminDraft>('admin-create', { username, role }, { initial: EMPTY_DRAFT, open });

  const resumeDraft = () => {
    if (!draft.draft) return;
    reset({ ...draft.draft.value, password: '' }, { keepDefaultValues: true });
    draft.dismiss();
  };

  const onSubmit = async (values: FormValues) => {
    try {
      await createAdmin.mutateAsync(values);
      draft.clear();
      reset(EMPTY_VALUES);
      onOpenChange(false);
      toast.add({ description: t('settings.admins_create_success'), type: 'success' });
    } catch (err) {
      if (err instanceof ApiError && err.code === 'conflict') {
        setError('username', { message: 'taken' });
        return;
      }
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        for (const field of Object.keys(err.fields)) {
          if (field === 'username' || field === 'password' || field === 'role') {
            setError(field, { message: err.fields[field] });
          }
        }
        return;
      }
      setError('root', { message: err instanceof ApiError ? err.message : t('common.error_generic') });
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset(EMPTY_VALUES);
        onOpenChange(next);
      }}
    >
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <div className="flex items-center gap-1.5">
            <DialogTitle>{t('settings.admins_create_title')}</DialogTitle>
            <HelpButton topic="settings.admins" className="-my-1.5" />
          </div>
        </DialogHeader>

        {draft.draft && <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />}

        <form className="space-y-4" onSubmit={(e) => void handleSubmit(onSubmit)(e)} noValidate>
          <div className="space-y-2">
            <Label htmlFor="admin-username">{t('settings.admins_field_username')}</Label>
            <Input
              id="admin-username"
              autoComplete="off"
              {...register('username')}
              aria-invalid={!!errors.username}
              aria-describedby={errors.username ? 'admin-username-error' : undefined}
            />
            {errors.username && (
              <p id="admin-username-error" className="text-label text-destructive">
                {t('settings.admins_username_error')}
              </p>
            )}
          </div>

          <div className="space-y-2">
            <Label htmlFor="admin-password">{t('settings.admins_field_password')}</Label>
            <Input
              id="admin-password"
              type="password"
              autoComplete="new-password"
              {...register('password')}
              aria-invalid={!!errors.password}
              aria-describedby="admin-password-hint"
            />
            <p id="admin-password-hint" className={cn('text-label', errors.password ? 'text-destructive' : 'text-mute')}>
              {t('settings.admins_password_hint')}
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="admin-role">{t('settings.admins_field_role')}</Label>
            <Select value={role} onValueChange={(v) => setValue('role', (v as AdminRole) ?? 'admin')}>
              <SelectTrigger id="admin-role" className="w-full">
                {/* Resolve the label explicitly - SelectValue only reflects a matched item's
                    rendered label once the popup has mounted at least once. */}
                <SelectValue>{(v: AdminRole) => t(ROLE_BADGE_KEY[v] ?? ROLE_BADGE_KEY.admin)}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="owner">{t('common.role_owner')}</SelectItem>
                <SelectItem value="admin">{t('common.role_admin')}</SelectItem>
                <SelectItem value="viewer">{t('common.role_viewer')}</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {errors.root && (
            <p role="alert" className="flex items-start gap-2 text-body text-destructive">
              <span className="mt-1.5 size-[7px] shrink-0 rounded-pill bg-err" aria-hidden="true" />
              <span className="min-w-0 flex-1">{errors.root.message}</span>
            </p>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={isSubmitting}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={isSubmitting}>
              {t('settings.admins_create_submit')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function AdminsForm() {
  const { t, i18n } = useTranslation();
  const { user } = useAuth();
  const adminsQuery = useAdmins();
  const deleteAdmin = useDeleteAdmin();

  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Admin | null>(null);

  const admins = adminsQuery.data?.items ?? [];

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await deleteAdmin.mutateAsync(deleteTarget.id);
      toast.add({ description: t('settings.admins_delete_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  if (adminsQuery.isLoading) {
    // The table that is coming, in silhouette, rather than a grey slab.
    return (
      <div className="max-w-2xl">
        <DataTableSkeleton columns={4} rows={3} />
      </div>
    );
  }

  if (adminsQuery.isError) {
    return (
      <div className="max-w-2xl">
        <ErrorState
          message={adminsQuery.error instanceof ApiError ? adminsQuery.error.message : t('common.error_generic')}
          retryLabel={t('common.refresh')}
          onRetry={() => void adminsQuery.refetch()}
        />
      </div>
    );
  }

  return (
    <div className="flex max-w-2xl flex-col gap-4">
      <Arriving>
        <Panel>
          <PanelHeader
            title={t('settings.tab_admins')}
            meta={String(admins.length)}
            actions={<HelpButton topic="settings.admins" />}
          />

          <div className="hidden md:block">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-4">{t('settings.admins_column_username')}</TableHead>
                  <TableHead>{t('settings.admins_column_role')}</TableHead>
                  <TableHead>{t('settings.admins_column_created')}</TableHead>
                  <TableHead className="w-0 pr-4" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {admins.map((a) => (
                  <TableRow key={a.id}>
                    <TableCell className="pl-4 font-medium text-foreground">{a.username}</TableCell>
                    <TableCell>
                      <RoleTag role={a.role} />
                    </TableCell>
                    <TableCell className="mono text-mono text-dim">
                      {a.created_at ? formatDate(a.created_at, i18n.language) : '—'}
                    </TableCell>
                    <TableCell className="pr-4 text-right">
                      {a.id !== user?.id && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => setDeleteTarget(a)}
                          aria-label={t('common.delete')}
                        >
                          <Trash2 />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>

          <ul className="divide-y divide-hairline md:hidden">
            {admins.map((a) => (
              <li key={a.id} className="flex items-center justify-between gap-2 px-4 py-3">
                <div className="min-w-0">
                  <p className="truncate text-body font-medium text-foreground">{a.username}</p>
                  <div className="mt-1 flex items-center gap-2">
                    <RoleTag role={a.role} />
                    <span className="mono text-mono text-dim">
                      {a.created_at ? formatDate(a.created_at, i18n.language) : '—'}
                    </span>
                  </div>
                </div>
                {a.id !== user?.id && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setDeleteTarget(a)}
                    aria-label={t('common.delete')}
                  >
                    <Trash2 />
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </Panel>
      </Arriving>

      <FormFooter>
        <Button type="button" variant="outline" onClick={() => setCreateOpen(true)}>
          <Plus />
          {t('settings.admins_add')}
        </Button>
      </FormFooter>

      <CreateAdminDialog open={createOpen} onOpenChange={setCreateOpen} />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('settings.admins_delete_confirm_title', { username: deleteTarget?.username ?? '' })}
        description={t('settings.admins_delete_confirm_description')}
        destructive
        confirmLabel={t('common.delete')}
        onConfirm={handleDelete}
      />
    </div>
  );
}
