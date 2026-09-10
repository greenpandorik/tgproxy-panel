import { zodResolver } from '@hookform/resolvers/zod';
import { SlidersHorizontal } from 'lucide-react';
import { useState } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { useBatchKeys, useCreateKey } from '@/api/keys';
import { useNodes } from '@/api/nodes';
import { DraftBanner } from '@/components/common/DraftBanner';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Textarea } from '@/components/ui/textarea';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';
import { formatDateTime } from '@/lib/format';
import { EMPTY_TELEMT_LIMITS_FORM, telemtLimitsFromForm, validateTelemtLimitsForm } from '@/lib/units';
import { cn } from '@/lib/utils';

import { countLimits } from './keyLimits';
import { KeyLimitsPanel } from './KeyLimitsPanel';
import { ZERO_LIMITS } from './LimitsFields';
import { NodeCapacityList } from './NodeCapacityList';
import { carrierSlug, transportScope } from './transport';
import { TransportField } from './TransportField';

import type { LimitFieldName } from './LimitsFields';
import type { TransportScope } from './transport';
import type { AccessKey, KeyInput, ProfileLimits } from '@/api/types';
import type { TelemtLimitsForm } from '@/lib/units';

const FORM_ID = 'create-key-form';

// The form is a stack of decisions, not a stack of fields.
const GROUP_CLASS = 'space-y-3 py-4 first:pt-0 last:pb-0';

const limitsShape = {
  max_sessions: z.number().int().min(0),
  max_streams: z.number().int().min(0),
  max_backend_dials_in_flight: z.number().int().min(0),
  new_sessions_per_minute: z.number().int().min(0),
  new_sessions_burst: z.number().int().min(0),
  new_streams_per_minute: z.number().int().min(0),
  new_streams_burst: z.number().int().min(0),
  max_streams_per_session: z.number().int().min(0),
  max_pending_per_session: z.number().int().min(0),
} as const;

const formSchema = z
  .object({
    mode: z.enum(['shared', 'personal', 'batch']),
    label: z.string(),
    owner_label: z.string(),
    prefix: z.string(),
    count: z.coerce.number().int(),
    carrier_mode: z.enum(['https', 'https-lanes', 'websocket', 'websocket-lanes']),
    node_ids: z.array(z.string()),
    expires_at: z.string(),
    note: z.string(),
    limits: z.object(limitsShape),
    telemt_limits: z.object({
      quota_gb: z.string(),
      rate_up_mbit: z.string(),
      rate_down_mbit: z.string(),
      max_unique_ips: z.string(),
      max_tcp_conns: z.string(),
    }),
  })
  .superRefine((val, ctx) => {
    if (val.mode !== 'batch' && val.label.trim() === '') {
      ctx.addIssue({ code: 'custom', path: ['label'], message: 'required' });
    }
    if (val.mode === 'personal' && val.owner_label.trim() === '') {
      ctx.addIssue({ code: 'custom', path: ['owner_label'], message: 'required' });
    }
    if (val.mode === 'batch') {
      if (val.prefix.trim() === '') ctx.addIssue({ code: 'custom', path: ['prefix'], message: 'required' });
      if (!Number.isFinite(val.count) || val.count < 1 || val.count > 100) {
        ctx.addIssue({ code: 'custom', path: ['count'], message: 'range' });
      }
    }
    if (val.node_ids.length === 0) {
      ctx.addIssue({ code: 'custom', path: ['node_ids'], message: 'required' });
    }
    if (val.expires_at.trim() !== '') {
      const d = new Date(val.expires_at);
      if (Number.isNaN(d.getTime()) || d.getTime() <= Date.now()) {
        ctx.addIssue({ code: 'custom', path: ['expires_at'], message: 'future' });
      }
    }
    const l = val.limits;
    if (l.max_streams > 0 && l.max_streams_per_session > l.max_streams) {
      ctx.addIssue({ code: 'custom', path: ['limits', 'max_streams_per_session'], message: 'exceeds_max_streams' });
    }
    if (l.max_streams > 0 && l.max_backend_dials_in_flight > l.max_streams) {
      ctx.addIssue({ code: 'custom', path: ['limits', 'max_backend_dials_in_flight'], message: 'exceeds_max_streams' });
    }
    for (const [name, message] of Object.entries(validateTelemtLimitsForm(val.telemt_limits))) {
      ctx.addIssue({ code: 'custom', path: ['telemt_limits', name], message });
    }
  });

type FormValues = z.infer<typeof formSchema>;

const defaultValues: FormValues = {
  mode: 'shared',
  label: '',
  owner_label: '',
  prefix: '',
  count: 1,
  carrier_mode: 'https',
  node_ids: [],
  expires_at: '',
  note: '',
  limits: { ...ZERO_LIMITS },
  telemt_limits: { ...EMPTY_TELEMT_LIMITS_FORM },
};

function useSummary(values: FormValues, scope: TransportScope, limitCount: number): string {
  const { t, i18n } = useTranslation();
  const parts: string[] = [];
  const name = values.label.trim() || t('keys.create_summary_no_name');

  if (values.mode === 'batch') {
    parts.push(t('keys.create_summary_batch', { prefix: values.prefix.trim() || name, count: values.count }));
  } else if (values.mode === 'personal') {
    parts.push(t('keys.create_summary_personal', { label: name }));
    if (values.owner_label.trim()) parts.push(t('keys.create_summary_owner', { owner: values.owner_label.trim() }));
  } else {
    parts.push(t('keys.create_summary_shared', { label: name }));
  }

  parts.push(
    values.node_ids.length === 0
      ? t('keys.create_summary_nodes_none')
      : t('keys.create_summary_nodes', { count: values.node_ids.length }),
  );

  const expires = values.expires_at.trim() ? new Date(values.expires_at) : null;
  parts.push(
    expires && !Number.isNaN(expires.getTime())
      ? t('keys.create_summary_expires', { date: formatDateTime(expires, i18n.language) })
      : t('keys.create_summary_no_expiry'),
  );

  if (scope === 'telemt') parts.push(t('keys.create_summary_transport_auto'));
  else if (scope !== 'none') {
    parts.push(t('keys.create_summary_transport_mode', { mode: t(`keys.carrier_${carrierSlug(values.carrier_mode)}`) }));
  }

  parts.push(
    limitCount === 0 ? t('keys.create_summary_limits_none') : t('keys.create_summary_limits_set', { count: limitCount }),
  );

  return parts.join(' · ');
}

interface CreateKeyDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** A single shared/personal key was created - caller opens the link dialog for it. */
  onCreated: (key: AccessKey) => void;
  /** A batch finished - caller shows the per-key result list. */
  onBatchCreated: (keys: AccessKey[]) => void;
}

/** Tabs to create a shared key, a personal key, or a prefixed batch of shared keys. */
export function CreateKeyDialog({ open, onOpenChange, onCreated, onBatchCreated }: CreateKeyDialogProps) {
  const { t } = useTranslation();
  const nodesQuery = useNodes();
  const createKey = useCreateKey();
  const batchKeys = useBatchKeys();
  const [view, setView] = useState<'main' | 'limits'>('main');

  const {
    control,
    register,
    handleSubmit,
    reset,
    watch,
    setValue,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues,
  });

  const values = watch();
  const mode = values.mode;
  const nodes = nodesQuery.data?.items ?? [];
  const scope = transportScope(nodes, values.node_ids);
  const limitCount = countLimits(values.limits, values.telemt_limits);
  const summary = useSummary(values, scope, limitCount);
  const limitsInvalid = !!errors.limits || !!errors.telemt_limits;

  // One draft for the whole dialog: the tab is a form value, so a batch left half-typed comes back as a batch.
  const draft = useDraft<FormValues>('key-create', values, { initial: defaultValues, open });

  const resumeDraft = () => {
    if (!draft.draft) return;
    reset(draft.draft.value, { keepDefaultValues: true });
    setView('main');
    draft.dismiss();
  };

  const close = () => {
    reset(defaultValues);
    setView('main');
    onOpenChange(false);
  };

  const clearLimits = () => {
    setValue('limits', { ...ZERO_LIMITS });
    setValue('telemt_limits', { ...EMPTY_TELEMT_LIMITS_FORM });
  };

  const limitsError = (name: LimitFieldName): string | undefined => {
    const msg = errors.limits?.[name]?.message;
    return msg ? t(`keys.validation_${msg}`) : undefined;
  };

  const telemtLimitsErrors = (): Partial<Record<keyof TelemtLimitsForm, string>> => {
    const out: Partial<Record<keyof TelemtLimitsForm, string>> = {};
    for (const name of Object.keys(EMPTY_TELEMT_LIMITS_FORM) as (keyof TelemtLimitsForm)[]) {
      const msg = errors.telemt_limits?.[name]?.message;
      if (msg) out[name] = t(`keys.validation_${msg}`);
    }
    return out;
  };

  const onSubmit = async (values: FormValues) => {
    const limits: ProfileLimits = { ...values.limits };
    const carrier = transportScope(nodes, values.node_ids) === 'telemt' ? 'https' : values.carrier_mode;
    const payload: KeyInput = {
      label: values.mode === 'batch' ? values.prefix.trim() : values.label.trim(),
      type: values.mode === 'personal' ? 'PERSONAL' : 'SHARED',
      owner_label: values.mode === 'personal' ? values.owner_label.trim() : undefined,
      note: values.note.trim() || undefined,
      carrier_mode: carrier,
      limits,
      telemt_limits: telemtLimitsFromForm(values.telemt_limits),
      expires_at: values.expires_at.trim() ? new Date(values.expires_at).toISOString() : undefined,
      node_ids: values.node_ids,
    };
    if (values.mode === 'batch') {
      payload.prefix = values.prefix.trim();
      payload.count = values.count;
    }

    try {
      if (values.mode === 'batch') {
        const result = await batchKeys.mutateAsync(payload);
        draft.clear();
        close();
        onBatchCreated(result.items);
      } else {
        const created = await createKey.mutateAsync(payload);
        draft.clear();
        close();
        onCreated(created);
      }
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        const known = ['label', 'owner_label', 'carrier_mode', 'node_ids', 'expires_at', 'prefix', 'count'] as const;
        const unmatched: string[] = [];
        for (const field of Object.keys(err.fields)) {
          if ((known as readonly string[]).includes(field)) {
            setError(field as (typeof known)[number], { message: err.fields[field] });
          } else {
            unmatched.push(`${field}: ${err.fields[field]}`);
          }
        }
        if (unmatched.length > 0) setError('root', { message: unmatched.join('; ') });
        return;
      }
      setError('root', { message: err instanceof ApiError ? err.message : t('keys.create_error') });
    }
  };

  const onInvalid = (failed: typeof errors) => {
    if (failed.limits || failed.telemt_limits) setView('limits');
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) close();
        else onOpenChange(next);
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <DialogTitle>{t('keys.create_title')}</DialogTitle>
            <HelpButton topic={mode === 'batch' ? 'keys.batch' : 'keys.create'} className="-my-1" />
          </div>
          <DialogDescription>{t('keys.create_description')}</DialogDescription>
        </DialogHeader>

        {view === 'main' && draft.draft && (
          <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />
        )}

        {view === 'main' && (
          <Controller
            control={control}
            name="mode"
            render={({ field }) => (
              <Tabs value={field.value} onValueChange={(v) => field.onChange(v as FormValues['mode'])}>
                <TabsList>
                  <TabsTrigger value="shared">{t('keys.tab_shared')}</TabsTrigger>
                  <TabsTrigger value="personal">{t('keys.tab_personal')}</TabsTrigger>
                  <TabsTrigger value="batch">{t('keys.tab_batch')}</TabsTrigger>
                </TabsList>
              </Tabs>
            )}
          />
        )}

        <form
          id={FORM_ID}
          className="max-h-[56vh] overflow-y-auto pr-1"
          onSubmit={(e) => void handleSubmit(onSubmit, onInvalid)(e)}
          noValidate
        >
          <div hidden={view !== 'main'} className="divide-y divide-hairline">
            {/* Who it is for. */}
            <section className={GROUP_CLASS}>
              {mode === 'batch' ? (
                <div className="grid grid-cols-1 gap-x-4 gap-y-3 sm:grid-cols-[1fr_auto]">
                  <div className="space-y-2">
                    <Label htmlFor="key-prefix">{t('keys.field_prefix')}</Label>
                    <Input id="key-prefix" autoFocus placeholder="vip" {...register('prefix')} aria-invalid={!!errors.prefix} />
                    <p className="text-label text-mute">{t('keys.field_prefix_hint')}</p>
                    {errors.prefix && <p className="text-label text-destructive">{t('common.required')}</p>}
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="key-count">{t('keys.field_count')}</Label>
                    <Input
                      id="key-count"
                      type="number"
                      min={1}
                      max={100}
                      className="mono w-24 text-mono"
                      {...register('count')}
                      aria-invalid={!!errors.count}
                    />
                    {errors.count && <p className="text-label text-destructive">{t('keys.validation_count')}</p>}
                  </div>
                </div>
              ) : (
                <div className="space-y-2">
                  <Label htmlFor="key-label">{t('keys.field_label')}</Label>
                  <Input id="key-label" autoFocus {...register('label')} aria-invalid={!!errors.label} />
                  {errors.label && <p className="text-label text-destructive">{t('common.required')}</p>}
                </div>
              )}

              {mode === 'personal' && (
                <div className="space-y-2">
                  <Label htmlFor="key-owner">{t('keys.field_owner_label')}</Label>
                  <Input id="key-owner" {...register('owner_label')} aria-invalid={!!errors.owner_label} />
                  <p className="text-label text-mute">{t('keys.field_owner_label_hint')}</p>
                  {errors.owner_label && <p className="text-label text-destructive">{t('common.required')}</p>}
                </div>
              )}
            </section>

            {/* Where it works. */}
            <section className={GROUP_CLASS}>
              <div className="space-y-2">
                <Label>{t('keys.field_nodes')}</Label>
                <Controller
                  control={control}
                  name="node_ids"
                  render={({ field }) => <NodeCapacityList nodes={nodes} selectedIds={field.value} onChange={field.onChange} />}
                />
                {errors.node_ids && <p className="text-label text-destructive">{t('keys.validation_node_ids')}</p>}
              </div>

              <Controller
                control={control}
                name="carrier_mode"
                render={({ field }) => <TransportField scope={scope} value={field.value} onChange={field.onChange} />}
              />
            </section>

            {/* How long it lasts. */}
            <section className={GROUP_CLASS}>
              <div className="space-y-2">
                <Label htmlFor="key-expires">
                  {t('keys.field_expires_at')} <span className="font-normal text-mute">({t('keys.field_optional')})</span>
                </Label>
                <Input
                  id="key-expires"
                  type="datetime-local"
                  className="mono w-fit text-mono"
                  {...register('expires_at')}
                  aria-invalid={!!errors.expires_at}
                />
                {errors.expires_at && <p className="text-label text-destructive">{t('keys.validation_expires_future')}</p>}
              </div>
            </section>

            {/* What it is allowed to do. */}
            <section className={GROUP_CLASS}>
              <button
                type="button"
                onClick={() => setView('limits')}
                className={cn(
                  'flex w-full items-center justify-between gap-4 rounded-control border border-hairline px-3 py-2 text-left transition-[background-color,scale] hover:bg-elevated active:scale-[0.99]',
                  limitsInvalid && 'border-destructive',
                )}
              >
                <span className="min-w-0">
                  <span className="block text-body text-foreground">{t('keys.limits_entry_label')}</span>
                  <span className={cn('block text-label', limitsInvalid ? 'text-destructive' : 'text-mute')}>
                    {limitsInvalid
                      ? t('keys.limits_entry_invalid')
                      : limitCount === 0
                        ? t('keys.limits_entry_none')
                        : t('keys.limits_entry_set', { count: limitCount })}
                  </span>
                </span>
                <SlidersHorizontal size={16} strokeWidth={1.8} aria-hidden="true" className="shrink-0 text-mute" />
              </button>
            </section>

            {/* Anything the operator wants to remember about it. */}
            <section className={GROUP_CLASS}>
              <div className="space-y-2">
                <Label htmlFor="key-note">
                  {t('keys.field_note')} <span className="font-normal text-mute">({t('keys.field_optional')})</span>
                </Label>
                <Textarea id="key-note" rows={2} {...register('note')} />
              </div>

              {errors.root && (
                <p role="alert" className="flex items-start gap-2 text-body text-destructive">
                  <span className="mt-2 size-[7px] shrink-0 rounded-pill bg-destructive" aria-hidden="true" />
                  {errors.root.message}
                </p>
              )}
            </section>
          </div>

          {view === 'limits' && (
            <Controller
              control={control}
              name="limits"
              render={({ field: limitsField }) => (
                <Controller
                  control={control}
                  name="telemt_limits"
                  render={({ field: telemtField }) => (
                    <KeyLimitsPanel
                      limits={limitsField.value}
                      onLimitsChange={limitsField.onChange}
                      telemt={telemtField.value}
                      onTelemtChange={telemtField.onChange}
                      telemtAvailable={scope === 'telemt' || scope === 'mixed'}
                      legacyAvailable={scope === 'tproxy' || scope === 'mixed'}
                      limitsErrors={{
                        max_sessions: limitsError('max_sessions'),
                        max_streams: limitsError('max_streams'),
                        max_backend_dials_in_flight: limitsError('max_backend_dials_in_flight'),
                        new_sessions_per_minute: limitsError('new_sessions_per_minute'),
                        new_sessions_burst: limitsError('new_sessions_burst'),
                        new_streams_per_minute: limitsError('new_streams_per_minute'),
                        new_streams_burst: limitsError('new_streams_burst'),
                        max_streams_per_session: limitsError('max_streams_per_session'),
                        max_pending_per_session: limitsError('max_pending_per_session'),
                      }}
                      telemtErrors={telemtLimitsErrors()}
                      onBack={() => setView('main')}
                    />
                  )}
                />
              )}
            />
          )}
        </form>

        {view === 'main' && (
          <div className="rounded-control bg-elevated px-3 py-2">
            <p className="micro text-mute">{t('keys.create_summary_title')}</p>
            <p className="mt-1 text-label text-foreground">{summary}</p>
          </div>
        )}

        {view === 'limits' ? (
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={clearLimits}>
              {t('keys.limits_edit_reset')}
            </Button>
            <Button type="button" onClick={() => setView('main')}>
              {t('keys.limits_edit_done')}
            </Button>
          </DialogFooter>
        ) : (
          <DialogFooter>
            <Button type="button" variant="outline" onClick={close} disabled={isSubmitting}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" form={FORM_ID} disabled={isSubmitting}>
              {mode === 'batch' ? t('keys.create_batch_submit') : t('keys.create_submit')}
            </Button>
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  );
}
