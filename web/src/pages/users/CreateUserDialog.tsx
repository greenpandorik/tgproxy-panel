import { zodResolver } from '@hookform/resolvers/zod';
import { CalendarClock, Gauge, UserRound } from 'lucide-react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';

import { useBatchKeys, useCreateKey } from '@/api/keys';
import { useNodes } from '@/api/nodes';
import { subscriptionBase, useSubscriptionService } from '@/api/subscriptionService';
import { DraftBanner } from '@/components/common/DraftBanner';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';

import { transportScope } from './transport';
import { AboutFields, AccessFields, LimitsCardBody, UserCard } from './UserCards';
import { NEW_USER, createPayload, userSchema } from './userForm';

import type { UserFormValues } from './userForm';
import type { AccessKey } from '@/api/types';

const FORM_ID = 'create-user-form';

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
  const draft = useDraft<UserFormValues>('user-create', values, { initial: NEW_USER, open });

  const resumeDraft = () => {
    if (!draft.draft) return;
    reset({ ...draft.draft.value, creating: true }, { keepDefaultValues: true });
    draft.dismiss();
  };

  const close = () => {
    reset(NEW_USER);
    onOpenChange(false);
  };

  const onSubmit = async (v: UserFormValues) => {
    const payload = createPayload(v, scope === 'telemt');
    try {
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
        for (const field of Object.keys(err.fields)) {
          if ((known as readonly string[]).includes(field)) {
            const msg = field === 'sub_slug' ? t(err.fields[field] === 'taken' ? 'users.validation_slug_taken' : 'users.validation_slug') : err.fields[field];
            setError(field as (typeof known)[number], { message: msg });
          } else {
            unmatched.push(`${field}: ${err.fields[field]}`);
          }
        }
        if (unmatched.length > 0) setError('root', { message: unmatched.join('; ') });
        return;
      }
      setError('root', { message: err instanceof ApiError ? err.message : t('users.create_error') });
    }
  };

  const mode = values.mode;

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
            <DialogTitle>{t(mode === 'batch' ? 'users.create_title_batch' : 'users.create_title')}</DialogTitle>
            <HelpButton topic={mode === 'batch' ? 'keys.batch' : 'keys.create'} className="-my-1" />
          </div>
        </DialogHeader>

        {draft.draft && <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />}

        <div className="space-y-3">
          <Controller
            control={control}
            name="mode"
            render={({ field }) => (
              <Tabs value={field.value} onValueChange={(v) => field.onChange(v as UserFormValues['mode'])}>
                <TabsList>
                  <TabsTrigger value="personal">{t('users.tab_personal')}</TabsTrigger>
                  <TabsTrigger value="shared">{t('users.tab_shared')}</TabsTrigger>
                  <TabsTrigger value="batch">{t('users.tab_batch')}</TabsTrigger>
                </TabsList>
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
            <UserCard icon={UserRound} title={t(mode === 'batch' ? 'users.card_names' : 'users.card_about')}>
              <AboutFields
                control={control}
                register={register}
                errors={errors}
                values={values}
                subscriptionBase={subscriptionBase(serviceQuery.data)}
              />
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
            {mode === 'batch' ? t('users.create_batch_submit', { count: values.count }) : t('users.create_submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
