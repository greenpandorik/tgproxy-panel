import { zodResolver } from '@hookform/resolvers/zod';
import { ChevronRight } from 'lucide-react';
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Textarea } from '@/components/ui/textarea';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';
import { EMPTY_TELEMT_LIMITS_FORM, telemtLimitsFromForm, validateTelemtLimitsForm } from '@/lib/units';
import { cn } from '@/lib/utils';

import { LimitsFields, ZERO_LIMITS } from './LimitsFields';
import { NodeCapacityList } from './NodeCapacityList';
import { TelemtLimitsFields } from './TelemtLimitsFields';

import type { LimitFieldName } from './LimitsFields';
import type { AccessKey, CarrierMode, KeyInput, ProfileLimits } from '@/api/types';
import type { TelemtLimitsForm } from '@/lib/units';

const CARRIER_MODES: CarrierMode[] = ['https', 'https-lanes', 'websocket', 'websocket-lanes'];

const FORM_ID = 'create-key-form';

// The form is a stack of decisions, not a stack of fields.
const GROUP_CLASS = 'space-y-3 py-4 first:pt-0 last:pb-0';

function carrierSlug(mode: CarrierMode): string {
  return mode.replace(/-/g, '_');
}

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
  const [showLimits, setShowLimits] = useState(false);

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
  const carrierMode = values.carrier_mode;
  const selectedNodeIds = values.node_ids;
  const nodes = nodesQuery.data?.items ?? [];

  // One draft for the whole dialog: the tab is a form value, so a batch left half-typed comes back as a batch.
  const draft = useDraft<FormValues>('key-create', values, { initial: defaultValues, open });

  const resumeDraft = () => {
    if (!draft.draft) return;
    const saved = draft.draft.value;
    reset(saved, { keepDefaultValues: true });
    setShowLimits(Object.values(saved.limits ?? {}).some((v) => Number(v) > 0));
    draft.dismiss();
  };
  const hasTelemtNode = nodes.some((n) => n.engine === 'telemt' && selectedNodeIds.includes(n.id));

  const close = () => {
    reset(defaultValues);
    setShowLimits(false);
    onOpenChange(false);
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
    const payload: KeyInput = {
      label: values.mode === 'batch' ? values.prefix.trim() : values.label.trim(),
      type: values.mode === 'personal' ? 'PERSONAL' : 'SHARED',
      owner_label: values.mode === 'personal' ? values.owner_label.trim() : undefined,
      note: values.note.trim() || undefined,
      carrier_mode: values.carrier_mode,
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

        {draft.draft && <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />}

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

        <form
          id={FORM_ID}
          className="max-h-[60vh] divide-y divide-hairline overflow-y-auto pr-1"
          onSubmit={(e) => void handleSubmit(onSubmit)(e)}
          noValidate
        >
          {/* What the key is called. */}
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

          {/* Where it lives, and how it travels. */}
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

            <div className="space-y-2">
              <Label htmlFor="key-carrier">{t('keys.field_carrier_mode')}</Label>
              <Select value={carrierMode} onValueChange={(v) => setValue('carrier_mode', v as CarrierMode)}>
                <SelectTrigger id="key-carrier" className="w-full">
                  {/* Resolve the label explicitly - SelectValue only reflects a matched item's
                      rendered label once the popup has mounted at least once, so it would
                      otherwise show the raw value ("https-lanes"). */}
                  <SelectValue>{(v: CarrierMode) => t(`keys.carrier_${carrierSlug(v ?? 'https')}`)}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {CARRIER_MODES.map((m) => (
                    <SelectItem key={m} value={m}>
                      {t(`keys.carrier_${carrierSlug(m)}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-label text-mute">{t(`keys.carrier_${carrierSlug(carrierMode)}_desc`)}</p>
            </div>
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

          {/* What it is allowed to do. Both blocks are limits, so they share one
              group and the advanced set stays folded away inside it. */}
          <section className={GROUP_CLASS}>
            <div className="flex items-center gap-2">
              <h3 className="micro text-mute">{t('keys.telemt_limits_title')}</h3>
              <HelpButton topic="keys.limits" className="-my-1" />
            </div>
            <Controller
              control={control}
              name="telemt_limits"
              render={({ field }) => (
                <TelemtLimitsFields
                  value={field.value}
                  onChange={field.onChange}
                  disabled={!hasTelemtNode}
                  errors={telemtLimitsErrors()}
                />
              )}
            />

            <div>
              <button
                type="button"
                onClick={() => setShowLimits((v) => !v)}
                aria-expanded={showLimits}
                className="-my-1 flex w-full items-center gap-2 py-1 text-label text-mute transition-[color,scale] outline-none hover:text-foreground focus-visible:text-foreground active:scale-[0.985]"
              >
                <ChevronRight className={cn('size-3.5 transition-transform', showLimits && 'rotate-90')} />
                {t('keys.field_limits_toggle')}
              </button>
              {showLimits && (
                <Controller
                  control={control}
                  name="limits"
                  render={({ field }) => (
                    <LimitsFields
                      className="mt-4"
                      value={field.value}
                      onChange={field.onChange}
                      errors={{
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
                    />
                  )}
                />
              )}
            </div>
          </section>

          {/* Anything the operator wants to remember about it. */}
          <section className={GROUP_CLASS}>
            <div className="space-y-2">
              <Label htmlFor="key-note">{t('keys.field_note')}</Label>
              <Textarea id="key-note" rows={2} {...register('note')} />
            </div>

            {errors.root && (
              <p role="alert" className="flex items-start gap-2 text-body text-destructive">
                <span className="mt-2 size-[7px] shrink-0 rounded-pill bg-destructive" aria-hidden="true" />
                {errors.root.message}
              </p>
            )}
          </section>
        </form>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={close} disabled={isSubmitting}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form={FORM_ID} disabled={isSubmitting}>
            {mode === 'batch' ? t('keys.create_batch_submit') : t('keys.create_submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
