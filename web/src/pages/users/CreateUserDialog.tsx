import { zodResolver } from '@hookform/resolvers/zod';
import { CalendarClock, Gauge, UserRound } from 'lucide-react';
import { useState } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';

import { useBatchKeys, useCreateKey, useImportKeys } from '@/api/keys';
import { useNodes } from '@/api/nodes';
import { subscriptionBase, useSubscriptionService } from '@/api/subscriptionService';
import { DraftBanner } from '@/components/common/DraftBanner';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';

import { ImportFields } from './ImportFields';
import { parseImport } from './importList';
import { transportScope } from './transport';
import { AboutFields, AccessFields, LimitsCardBody, UserCard } from './UserCards';
import { NEW_USER, createPayload, importPayload, userSchema } from './userForm';

import type { UserFormValues } from './userForm';
import type { AccessKey } from '@/api/types';

const FORM_ID = 'create-user-form';

type Mode = UserFormValues['mode'];

const TITLES: Record<Mode, string> = {
  personal: 'users.create_title',
  shared: 'users.create_title',
  batch: 'users.create_title_batch',
  import: 'users.create_title_import',
};

const CARD_TITLES: Record<Mode, string> = {
  personal: 'users.card_about',
  shared: 'users.card_about',
  batch: 'users.card_names',
  import: 'users.card_import',
};

const HELP = { personal: 'keys.create', shared: 'keys.create', batch: 'keys.batch', import: 'keys.import' } as const;

interface CreateUserDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (user: AccessKey) => void;
  onBatchCreated: (users: AccessKey[]) => void;
}

export function CreateUserDialog({ open, onOpenChange, onCreated, onBatchCreated }: CreateUserDialogProps) {
  const { t } = useTranslation();
  const nodesQuery = useNodes();
  const createKey = useCreateKey();
  const batchKeys = useBatchKeys();
  const importKeys = useImportKeys();
  const [lineErrors, setLineErrors] = useState<{ text: string; byLine: Record<number, string> } | null>(null);
  const serviceQuery = useSubscriptionService();

  const {
    control,
    register,
    handleSubmit,
    reset,
    watch,
    setValue,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<UserFormValues>({ resolver: zodResolver(userSchema), defaultValues: NEW_USER });

  const values = watch();
  const nodes = nodesQuery.data?.items ?? [];
  const scope = transportScope(nodes, values.node_ids);
  const draft = useDraft<UserFormValues>('user-create', { ...values, import_text: '' }, { initial: NEW_USER, open });
  const mode = values.mode;
  const importCount = mode === 'import' ? parseImport(values.import_text).ready.length : 0;
  const shownLineErrors = lineErrors && lineErrors.text === values.import_text ? lineErrors.byLine : {};

  const resumeDraft = () => {
    if (!draft.draft) return;
    reset({ ...NEW_USER, ...draft.draft.value, import_text: '', creating: true }, { keepDefaultValues: true });
    draft.dismiss();
  };

  const close = () => {
    reset(NEW_USER);
    setLineErrors(null);
    onOpenChange(false);
  };

  const onSubmit = async (v: UserFormValues) => {
    const payload = createPayload(v, scope === 'telemt');
    try {
      if (v.mode === 'import') {
        const result = await importKeys.mutateAsync(importPayload(v, scope === 'telemt'));
        draft.clear();
        close();
        onBatchCreated(result.items);
        return;
      }
      if (v.mode === 'batch') {
        const result = await batchKeys.mutateAsync(payload);
        draft.clear();
        close();
        onBatchCreated(result.items);
        return;
      }
      const created = await createKey.mutateAsync(payload);
      draft.clear();
      close();
      onCreated(created);
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        const known = ['label', 'owner_label', 'carrier_mode', 'node_ids', 'expires_at', 'prefix', 'count', 'sub_slug'] as const;
        const unmatched: string[] = [];
        const ready = v.mode === 'import' ? parseImport(v.import_text).ready : [];
        const byLine: Record<number, string> = {};
        for (const field of Object.keys(err.fields)) {
          const item = /^items\.(\d+)$/.exec(field);
          if (item && ready[Number(item[1])]) {
            const msg = err.fields[field];
            byLine[ready[Number(item[1])].line] = msg.includes('another user') ? t('users.import_taken') : msg;
          } else if ((known as readonly string[]).includes(field)) {
            const msg = field === 'sub_slug' ? t(err.fields[field] === 'taken' ? 'users.validation_slug_taken' : 'users.validation_slug') : err.fields[field];
            setError(field as (typeof known)[number], { message: msg });
          } else {
            unmatched.push(`${field}: ${err.fields[field]}`);
          }
        }
        if (Object.keys(byLine).length > 0) {
          setLineErrors({ text: v.import_text, byLine });
          unmatched.unshift(t('users.import_fix_lines'));
        }
        if (unmatched.length > 0) setError('root', { message: unmatched.join('; ') });
        return;
      }
      setError('root', { message: err instanceof ApiError ? err.message : t('users.create_error') });
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) close();
        else onOpenChange(next);
      }}
    >
      <DialogContent className="max-h-[94vh] overflow-y-auto sm:max-w-5xl">
        <DialogHeader>
          <div className="flex items-center gap-2 pr-8">
            <DialogTitle>{t(TITLES[mode])}</DialogTitle>
            <HelpButton topic={HELP[mode]} className="-my-1" />
          </div>
        </DialogHeader>

        {draft.draft && <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />}

        <div className="min-w-0 space-y-3">
          <Controller
            control={control}
            name="mode"
            render={({ field }) => (
              <Tabs value={field.value} onValueChange={(v) => field.onChange(v as UserFormValues['mode'])}>
                <div className="overflow-x-auto [scrollbar-width:none]">
                  <TabsList className="w-max min-w-full">
                    <TabsTrigger value="personal">{t('users.tab_personal')}</TabsTrigger>
                    <TabsTrigger value="shared">{t('users.tab_shared')}</TabsTrigger>
                    <TabsTrigger value="batch">{t('users.tab_batch')}</TabsTrigger>
                    <TabsTrigger value="import">{t('users.tab_import')}</TabsTrigger>
                  </TabsList>
                </div>
              </Tabs>
            )}
          />
          <DialogDescription>{t(`users.create_hint_${mode}`)}</DialogDescription>
        </div>

        <form
          id={FORM_ID}
          className="grid items-start gap-4 lg:grid-cols-2"
          onSubmit={(e) => void handleSubmit(onSubmit)(e)}
          noValidate
        >
          <div className="flex min-w-0 flex-col gap-4">
            <UserCard icon={UserRound} title={t(CARD_TITLES[mode])}>
              {mode === 'import' ? (
                <ImportFields register={register} errors={errors} text={values.import_text} lineErrors={shownLineErrors} />
              ) : (
                <AboutFields
                  control={control}
                  register={register}
                  errors={errors}
                  values={values}
                  subscriptionBase={subscriptionBase(serviceQuery.data)}
                />
              )}
            </UserCard>
          </div>
          <div className="flex min-w-0 flex-col gap-4">
            <UserCard icon={CalendarClock} title={t('users.card_access')}>
              <AccessFields
                control={control}
                register={register}
                errors={errors}
                values={values}
                setValue={setValue}
                nodes={nodes}
                scope={scope}
              />
            </UserCard>
            <UserCard icon={Gauge} title={t('users.card_limits')} actions={<HelpButton topic="keys.limits" />}>
              <LimitsCardBody control={control} errors={errors} scope={scope} />
            </UserCard>
          </div>
        </form>

        {errors.root && (
          <p role="alert" className="flex items-start gap-2 text-body text-destructive">
            <span className="mt-2 size-[7px] shrink-0 rounded-pill bg-destructive" aria-hidden="true" />
            {errors.root.message}
          </p>
        )}

        <DialogFooter className="flex-row justify-end">
          <Button type="button" variant="outline" onClick={close} disabled={isSubmitting}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form={FORM_ID} disabled={isSubmitting}>
            {mode === 'batch'
              ? t('users.create_batch_submit', { count: values.count })
              : mode === 'import'
                ? t('users.import_submit', { count: importCount })
                : t('users.create_submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
