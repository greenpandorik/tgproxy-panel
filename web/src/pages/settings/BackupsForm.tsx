import { zodResolver } from '@hookform/resolvers/zod';
import { Download, Trash2 } from 'lucide-react';
import { useEffect, useState } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { backupDownloadURL, useBackups, useCreateBackup, useDeleteBackup } from '@/api/backups';
import { usePutSettings, useSettings } from '@/api/settings';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { PanelEmpty } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Badge } from '@/components/ui/badge';
import { Button, buttonVariants } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import { Switch } from '@/components/ui/switch';
import { toast } from '@/components/ui/toast';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { formatBytes, formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import { Arriving, FormFooter } from './formShell';

import type { Backup, Settings } from '@/api/types';

const schema = z.object({
  enabled: z.boolean(),
  hour: z.coerce.number().int().min(0).max(23),
  keep: z.coerce.number().int().min(1).max(60),
});

type FormValues = z.infer<typeof schema>;

const HOURS = Array.from({ length: 24 }, (_, h) => h);
const hourLabel = (h: number) => `${String(h).padStart(2, '0')}:00 UTC`;

function valuesFromSettings(s: Settings): FormValues {
  return { enabled: s.backup_schedule.enabled, hour: s.backup_schedule.hour, keep: s.backup_schedule.keep };
}

/** Where the dump came from - a mono tag, like every other enum in the panel. */
function KindTag({ kind }: { kind: Backup['kind'] }) {
  const { t } = useTranslation();
  return <Badge>{t(kind === 'scheduled' ? 'settings.backups_kind_scheduled' : 'settings.backups_kind_manual')}</Badge>;
}

export function BackupsForm() {
  const { t, i18n } = useTranslation();
  const backupsQuery = useBackups();
  const settingsQuery = useSettings();
  const createBackup = useCreateBackup();
  const deleteBackup = useDeleteBackup();
  const putSettings = usePutSettings();

  const [deleteTarget, setDeleteTarget] = useState<Backup | null>(null);

  const {
    control,
    register,
    handleSubmit,
    reset,
    watch,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { enabled: false, hour: 3, keep: 7 },
  });

  useEffect(() => {
    if (settingsQuery.data) reset(valuesFromSettings(settingsQuery.data));
  }, [settingsQuery.data, reset]);

  const scheduleEnabled = watch('enabled');
  const backups = backupsQuery.data?.items ?? [];

  const onCreate = async () => {
    try {
      const created = await createBackup.mutateAsync();
      toast.add({ description: t('settings.backups_create_success', { name: created.name }), type: 'success' });
    } catch (err) {
      // backup_running is the only expected failure: the panel takes one dump at
      // a time, so say so instead of showing the generic error.
      const description =
        err instanceof ApiError && err.code === 'backup_running'
          ? t('settings.backups_create_running')
          : err instanceof ApiError
            ? err.message
            : t('common.error_generic');
      toast.add({ description, type: 'error' });
    }
  };

  const onDelete = async () => {
    if (!deleteTarget) return;
    try {
      await deleteBackup.mutateAsync(deleteTarget.id);
      toast.add({ description: t('settings.backups_delete_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const onSaveSchedule = async (values: FormValues) => {
    try {
      await putSettings.mutateAsync({ backup_schedule: values });
      toast.add({ description: t('settings.backups_schedule_saved'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  return (
    // Wider than the other six forms only because it holds a five-column table;
    // the head/body/footer shape underneath is the same.
    <div className="flex max-w-3xl flex-col gap-4">
      <Arriving>
        <Panel>
          <PanelHeader
            title={t('settings.backups_title')}
            meta={backups.length > 0 ? String(backups.length) : undefined}
            actions={
              <>
                <HelpButton topic="settings.backups" />
                <Button type="button" size="sm" onClick={() => void onCreate()} disabled={createBackup.isPending}>
                  {createBackup.isPending ? t('settings.backups_creating') : t('settings.backups_create')}
                </Button>
              </>
            }
          />

          <PanelBody className="border-b border-hairline py-3">
            <p className="max-w-prose text-label text-mute">{t('settings.backups_master_key_note')}</p>
          </PanelBody>

          {backupsQuery.isLoading ? (
            // Three rows of the table that is coming, not a grey block.
            <PanelBody className="space-y-3">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-4 w-full" />
              ))}
            </PanelBody>
          ) : backupsQuery.isError ? (
            <ErrorState
              inset
              message={backupsQuery.error instanceof ApiError ? backupsQuery.error.message : t('common.error_generic')}
              retryLabel={t('common.refresh')}
              onRetry={() => void backupsQuery.refetch()}
            />
          ) : backups.length === 0 ? (
            <PanelEmpty>{t('settings.backups_empty')}</PanelEmpty>
          ) : (
            <>
              <div className="hidden md:block">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="pl-4">{t('settings.backups_column_name')}</TableHead>
                      <TableHead className="text-right">{t('settings.backups_column_size')}</TableHead>
                      <TableHead>{t('settings.backups_column_kind')}</TableHead>
                      <TableHead>{t('settings.backups_column_created')}</TableHead>
                      <TableHead className="w-0 pr-4" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {backups.map((b) => (
                      <TableRow key={b.id}>
                        <TableCell className="mono pl-4 text-mono text-foreground">{b.name}</TableCell>
                        <TableCell className="mono text-right text-mono text-mute">{formatBytes(b.size)}</TableCell>
                        <TableCell>
                          <KindTag kind={b.kind} />
                        </TableCell>
                        <TableCell className="mono text-mono text-dim">{formatDateTime(b.created_at, i18n.language)}</TableCell>
                        <TableCell className="pr-4">
                          <div className="flex items-center justify-end gap-1">
                            <a
                              className={buttonVariants({ variant: 'ghost', size: 'icon-sm' })}
                              href={backupDownloadURL(b.id)}
                              download={b.name}
                              aria-label={t('settings.backups_download')}
                              title={t('settings.backups_download')}
                            >
                              <Download />
                            </a>
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon-sm"
                              onClick={() => setDeleteTarget(b)}
                              aria-label={t('common.delete')}
                            >
                              <Trash2 />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>

              <ul className="divide-y divide-hairline md:hidden">
                {backups.map((b) => (
                  <li key={b.id} className="flex items-center justify-between gap-2 px-4 py-3">
                    <div className="min-w-0">
                      <p className="mono truncate text-mono text-foreground">{b.name}</p>
                      <div className="mt-1 flex flex-wrap items-center gap-2">
                        <KindTag kind={b.kind} />
                        <span className="mono text-mono text-mute">{formatBytes(b.size)}</span>
                        <span className="mono text-mono text-dim">{formatDateTime(b.created_at, i18n.language)}</span>
                      </div>
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                      <a
                        className={buttonVariants({ variant: 'ghost', size: 'icon-sm' })}
                        href={backupDownloadURL(b.id)}
                        download={b.name}
                        aria-label={t('settings.backups_download')}
                      >
                        <Download />
                      </a>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => setDeleteTarget(b)}
                        aria-label={t('common.delete')}
                      >
                        <Trash2 />
                      </Button>
                    </div>
                  </li>
                ))}
              </ul>
            </>
          )}
        </Panel>
      </Arriving>

      <form className="flex flex-col gap-4" onSubmit={(e) => void handleSubmit(onSaveSchedule)(e)} noValidate>
        <Arriving index={1}>
          <Panel>
            <PanelHeader title={t('settings.backups_schedule_title')} actions={<HelpButton topic="settings.backups" />} />
            <PanelBody className="max-w-sm space-y-4">
              <p className="text-label text-mute">{t('settings.backups_schedule_hint')}</p>

              <div className="flex items-center justify-between gap-3">
                <Label htmlFor="backup-schedule-enabled">{t('settings.backups_schedule_enabled')}</Label>
                <Controller
                  control={control}
                  name="enabled"
                  render={({ field }) => (
                    <Switch id="backup-schedule-enabled" checked={field.value} onCheckedChange={field.onChange} />
                  )}
                />
              </div>

              {scheduleEnabled && (
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div className="space-y-2">
                    <Label htmlFor="backup-schedule-hour">{t('settings.backups_schedule_hour')}</Label>
                    <Controller
                      control={control}
                      name="hour"
                      render={({ field }) => (
                        <Select value={String(field.value)} onValueChange={(v) => field.onChange(Number(v ?? 0))}>
                          <SelectTrigger id="backup-schedule-hour" className="mono w-full text-mono">
                            <SelectValue>{(v: string) => hourLabel(Number(v))}</SelectValue>
                          </SelectTrigger>
                          <SelectContent>
                            {HOURS.map((h) => (
                              <SelectItem key={h} value={String(h)} className="mono text-mono">
                                {hourLabel(h)}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      )}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="backup-schedule-keep">{t('settings.backups_schedule_keep')}</Label>
                    <Input
                      id="backup-schedule-keep"
                      type="number"
                      min={1}
                      max={60}
                      className="mono text-mono"
                      {...register('keep')}
                      aria-invalid={!!errors.keep}
                      aria-describedby="backup-schedule-keep-hint"
                    />
                    <p
                      id="backup-schedule-keep-hint"
                      className={cn('text-label', errors.keep ? 'text-destructive' : 'text-mute')}
                    >
                      {t('settings.backups_schedule_keep_hint')}
                    </p>
                  </div>
                </div>
              )}
            </PanelBody>
          </Panel>
        </Arriving>

        <FormFooter>
          <Button type="submit" disabled={isSubmitting || !isDirty}>
            {t('common.save')}
          </Button>
        </FormFooter>
      </form>

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('settings.backups_delete_confirm_title', { name: deleteTarget?.name ?? '' })}
        description={t('settings.backups_delete_confirm_description')}
        destructive
        confirmLabel={t('common.delete')}
        onConfirm={onDelete}
      />
    </div>
  );
}
