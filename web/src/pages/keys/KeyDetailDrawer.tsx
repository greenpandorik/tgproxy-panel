import { zodResolver } from '@hookform/resolvers/zod';
import { Plus, X } from 'lucide-react';
import { useEffect, useState } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { useBindKey, useKey, usePatchKey, useRevokeKey, useRotateKey, useUnbindKey } from '@/api/keys';
import { useNodes } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { DraftBanner } from '@/components/common/DraftBanner';
import { StatusBadge } from '@/components/common/StatusBadge';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ENTER_CLASS, enter, enterDelay } from '@/components/ui/motion';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import { Skeleton } from '@/components/ui/skeleton';
import { Textarea } from '@/components/ui/textarea';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';
import { formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';
import { EMPTY_TELEMT_LIMITS_FORM, telemtLimitsFromForm, telemtLimitsToForm, validateTelemtLimitsForm } from '@/lib/units';
import { capacityText } from '@/pages/nodes/nodeDisplay';

import { KeyStatsSection } from './KeyStatsSection';
import { LimitsFields, ZERO_LIMITS } from './LimitsFields';
import { isNodeFull } from './NodeCapacityList';
import { SubscriptionLinkSection } from './SubscriptionLinkSection';
import { TelemtLimitsFields } from './TelemtLimitsFields';

import type { CSSProperties, ReactNode } from 'react';
import type { LimitFieldName } from './LimitsFields';
import type { AccessKey, CarrierMode } from '@/api/types';
import type { TelemtLimitsForm } from '@/lib/units';

const CARRIER_MODES: CarrierMode[] = ['https', 'https-lanes', 'websocket', 'websocket-lanes'];

function carrierSlug(mode: CarrierMode): string {
  return mode.replace(/-/g, '_');
}

/** Formats a Date as the local "YYYY-MM-DDTHH:mm" value an <input type="datetime-local"> expects. */
function toLocalInputValue(iso: string | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

const formSchema = z
  .object({
    label: z.string(),
    owner_label: z.string(),
    note: z.string(),
    carrier_mode: z.enum(['https', 'https-lanes', 'websocket', 'websocket-lanes']),
    no_expiry: z.boolean(),
    expires_at: z.string(),
    limits: z.object({
      max_sessions: z.number().int().min(0),
      max_streams: z.number().int().min(0),
      max_backend_dials_in_flight: z.number().int().min(0),
      new_sessions_per_minute: z.number().int().min(0),
      new_sessions_burst: z.number().int().min(0),
      new_streams_per_minute: z.number().int().min(0),
      new_streams_burst: z.number().int().min(0),
      max_streams_per_session: z.number().int().min(0),
      max_pending_per_session: z.number().int().min(0),
    }),
    telemt_limits: z.object({
      quota_gb: z.string(),
      rate_up_mbit: z.string(),
      rate_down_mbit: z.string(),
      max_unique_ips: z.string(),
      max_tcp_conns: z.string(),
    }),
  })
  .superRefine((val, ctx) => {
    if (val.label.trim() === '') ctx.addIssue({ code: 'custom', path: ['label'], message: 'required' });
    if (!val.no_expiry && val.expires_at.trim() === '') {
      ctx.addIssue({ code: 'custom', path: ['expires_at'], message: 'required' });
    }
    if (!val.no_expiry && val.expires_at.trim() !== '') {
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

/** What the form holds until the key arrives. */
const EMPTY_VALUES: FormValues = {
  label: '',
  owner_label: '',
  note: '',
  carrier_mode: 'https',
  no_expiry: true,
  expires_at: '',
  limits: { ...ZERO_LIMITS },
  telemt_limits: { ...EMPTY_TELEMT_LIMITS_FORM },
};

function valuesFromKey(key: AccessKey): FormValues {
  return {
    label: key.label,
    owner_label: key.owner_label,
    note: key.note,
    carrier_mode: key.carrier_mode,
    no_expiry: !key.expires_at,
    expires_at: toLocalInputValue(key.expires_at),
    limits: { ...ZERO_LIMITS, ...key.limits },
    telemt_limits: telemtLimitsToForm(key.telemt_limits),
  };
}

function Section({
  title,
  actions,
  className,
  style,
  children,
}: {
  title: string;
  actions?: ReactNode;
  className?: string;
  style?: CSSProperties;
  children: ReactNode;
}) {
  return (
    <section className={cn('space-y-3 border-t border-hairline pt-4', className)} style={style}>
      <div className="flex items-center gap-2">
        <h3 className="micro text-mute">{title}</h3>
        {actions}
      </div>
      {children}
    </section>
  );
}

interface KeyDetailDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  keyId: string | null;
}

export function KeyDetailDrawer({ open, onOpenChange, keyId }: KeyDetailDrawerProps) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const keyQuery = useKey(keyId ?? '');
  const key = keyQuery.data;
  const patchKey = usePatchKey(keyId ?? '');
  const bindKey = useBindKey(keyId ?? '');
  const unbindKey = useUnbindKey(keyId ?? '');
  const revokeKey = useRevokeKey();
  const rotateKey = useRotateKey();
  const nodesQuery = useNodes();

  const [addNodeId, setAddNodeId] = useState('');
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [rotateOpen, setRotateOpen] = useState(false);

  const {
    control,
    register,
    handleSubmit,
    reset,
    watch,
    setValue,
    setError,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: EMPTY_VALUES,
  });

  useEffect(() => {
    if (key) reset(valuesFromKey(key));
  }, [key, reset]);

  const values = watch();
  const noExpiry = values.no_expiry;
  const carrierMode = values.carrier_mode;

  const revoked = key?.status === 'revoked';
  const locked = revoked || !isWriter;

  // One draft per key; nothing is offered or kept while the form is locked.
  const draft = useDraft<FormValues>(`key-edit-${keyId ?? 'none'}`, values, {
    initial: key ? valuesFromKey(key) : EMPTY_VALUES,
    open: open && !!key && !locked,
  });

  const resumeDraft = () => {
    if (!draft.draft) return;
    // Keep the server values as the defaults, so the form counts as dirty and Save enables.
    reset(draft.draft.value, { keepDefaultValues: true });
    draft.dismiss();
  };
  const boundNodeIds = new Set((key?.nodes ?? []).map((n) => n.node_id));
  const allNodes = nodesQuery.data?.items ?? [];
  const addableNodes = allNodes.filter((n) => !boundNodeIds.has(n.id));
  const hasTelemtNode = allNodes.some((n) => n.engine === 'telemt' && boundNodeIds.has(n.id));
  const hasTproxyNode = allNodes.some((n) => n.engine === 'tproxy' && boundNodeIds.has(n.id));

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
    try {
      await patchKey.mutateAsync({
        label: values.label.trim(),
        owner_label: values.owner_label.trim(),
        note: values.note.trim(),
        carrier_mode: values.carrier_mode,
        limits: values.limits,
        telemt_limits: telemtLimitsFromForm(values.telemt_limits),
        clear_expiry: values.no_expiry,
        expires_at: values.no_expiry ? undefined : new Date(values.expires_at).toISOString(),
      });
      draft.clear();
      toast.add({ description: t('keys.edit_success'), type: 'success' });
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        const known = ['label', 'owner_label', 'carrier_mode', 'expires_at'] as const;
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
      setError('root', { message: err instanceof ApiError ? err.message : t('common.error_generic') });
    }
  };

  const handleAddNode = async () => {
    if (!addNodeId) return;
    try {
      await bindKey.mutateAsync(addNodeId);
      setAddNodeId('');
      toast.add({ description: t('keys.binding_added'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleRemoveNode = async (nodeId: string) => {
    try {
      await unbindKey.mutateAsync(nodeId);
      toast.add({ description: t('keys.binding_removed'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleRevoke = async () => {
    if (!key) return;
    try {
      await revokeKey.mutateAsync(key.id);
      toast.add({ description: t('keys.revoke_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleRotate = async () => {
    if (!key) return;
    try {
      await rotateKey.mutateAsync(key.id);
      toast.add({ description: t('keys.rotate_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-lg">
        <SheetHeader>
          <div className="flex items-center gap-2 pr-8">
            <SheetTitle className="truncate">{key ? key.label : t('keys.edit_title')}</SheetTitle>
            <HelpButton topic="keys.detail" className="-my-1" />
          </div>
          {key && (
            <SheetDescription className="flex flex-wrap items-center gap-x-3 gap-y-2 pt-1">
              <StatusBadge status={key.status} />
              <Badge>{key.type}</Badge>
              <span className="mono text-mono text-mute">{formatDateTime(key.created_at, i18n.language)}</span>
            </SheetDescription>
          )}
        </SheetHeader>

        {keyQuery.isLoading || !key ? (
          <div className="space-y-4 px-4 pb-4">
            {[0, 1, 2, 3].map((i) => (
              <div key={i} className="space-y-2">
                <Skeleton className="h-3 w-24" />
                <Skeleton className="h-8 w-full" />
              </div>
            ))}
            <div className="space-y-2">
              <Skeleton className="h-3 w-24" />
              <Skeleton className="h-16 w-full" />
            </div>
          </div>
        ) : (
          <div className="space-y-4 px-4 pb-4">
            {revoked && (
              <p role="alert" className="flex items-start gap-2 text-label text-destructive">
                <span className="mt-1 size-[7px] shrink-0 rounded-pill bg-destructive" aria-hidden="true" />
                {t('keys.edit_revoked_notice')}
              </p>
            )}

            {draft.draft && <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />}

            <form
              className={cn(ENTER_CLASS, 'space-y-4')}
              style={enterDelay(0)}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
              noValidate
            >
              <div className="space-y-2">
                <Label htmlFor="edit-label">{t('keys.field_label')}</Label>
                <Input id="edit-label" disabled={locked} {...register('label')} aria-invalid={!!errors.label} />
                {errors.label && <p className="text-label text-destructive">{t('common.required')}</p>}
              </div>

              {key.type === 'PERSONAL' && (
                <div className="space-y-2">
                  <Label htmlFor="edit-owner">{t('keys.field_owner_label')}</Label>
                  <Input id="edit-owner" disabled={locked} {...register('owner_label')} aria-invalid={!!errors.owner_label} />
                </div>
              )}

              <div className="space-y-2">
                <Label htmlFor="edit-carrier">{t('keys.field_carrier_mode')}</Label>
                <Select
                  value={carrierMode}
                  onValueChange={(v) => setValue('carrier_mode', v as CarrierMode, { shouldDirty: true })}
                  disabled={locked}
                >
                  <SelectTrigger id="edit-carrier" className="w-full">
                    {/* Resolve the label explicitly - SelectValue only reflects a matched
                        item's rendered label once the popup has mounted at least once, so
                        it would otherwise show the raw value ("https-lanes"). */}
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

              <div className="space-y-2">
                <div className="flex items-center justify-between gap-3">
                  <Label htmlFor="edit-expires">{t('keys.field_expires_at')}</Label>
                  <label className="flex items-center gap-2 text-label text-mute">
                    <Controller
                      control={control}
                      name="no_expiry"
                      render={({ field }) => (
                        <Checkbox checked={field.value} disabled={locked} onCheckedChange={(v) => field.onChange(!!v)} />
                      )}
                    />
                    {t('keys.field_no_expiry')}
                  </label>
                </div>
                <Input
                  id="edit-expires"
                  type="datetime-local"
                  className="mono"
                  disabled={noExpiry || locked}
                  {...register('expires_at')}
                  aria-invalid={!!errors.expires_at}
                />
                {errors.expires_at && (
                  <p className="text-label text-destructive">
                    {t(errors.expires_at.message === 'required' ? 'common.required' : 'keys.validation_expires_future')}
                  </p>
                )}
              </div>

              <div className="space-y-2">
                <Label htmlFor="edit-note">{t('keys.field_note')}</Label>
                <Textarea id="edit-note" rows={2} disabled={locked} {...register('note')} />
              </div>

              <Section title={t('keys.telemt_limits_title')} actions={<HelpButton topic="keys.limits" className="-my-1" />}>
                <Controller
                  control={control}
                  name="telemt_limits"
                  render={({ field }) => (
                    <TelemtLimitsFields
                      value={field.value}
                      onChange={field.onChange}
                      disabled={locked || !hasTelemtNode}
                      errors={telemtLimitsErrors()}
                    />
                  )}
                />
              </Section>

              <Section title={t('keys.field_limits_toggle')} actions={<HelpButton topic="keys.limits" className="-my-1" />}>
                <Controller
                  control={control}
                  name="limits"
                  render={({ field }) => (
                    <LimitsFields
                      value={field.value}
                      onChange={field.onChange}
                      telemtOnly={hasTelemtNode && !hasTproxyNode}
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
              </Section>

              {errors.root && (
                <p role="alert" className="flex items-start gap-2 text-body text-destructive">
                  <span className="mt-2 size-[7px] shrink-0 rounded-pill bg-destructive" aria-hidden="true" />
                  {errors.root.message}
                </p>
              )}

              {isWriter && (
                <div className="flex justify-end">
                  <Button type="submit" disabled={isSubmitting || locked || !isDirty}>
                    {t('common.save')}
                  </Button>
                </div>
              )}
            </form>

            <Section title={t('keys.field_nodes')} {...enter(1)}>
              <ul className="divide-y divide-hairline rounded-control border border-hairline empty:hidden">
                {key.nodes.map((n) => (
                  <li key={n.node_id} className="flex items-center justify-between gap-3 px-3 py-2">
                    <span className="min-w-0">
                      <span className="block truncate text-body text-foreground">{n.node_name}</span>
                      <span className="mono block truncate text-mono text-mute">{n.hostname}</span>
                    </span>
                    {isWriter && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        className="shrink-0"
                        disabled={locked || unbindKey.isPending}
                        onClick={() => void handleRemoveNode(n.node_id)}
                        aria-label={t('keys.binding_remove')}
                      >
                        <X />
                      </Button>
                    )}
                  </li>
                ))}
              </ul>

              {!locked && (
                <div className="flex items-center gap-2">
                  <Select value={addNodeId} onValueChange={(v) => setAddNodeId(v ?? '')}>
                    <SelectTrigger className="w-full">
                      {/* Resolve the label explicitly - SelectValue would otherwise show the
                          raw node id (a UUID) until the popup has mounted at least once. */}
                      <SelectValue placeholder={t('keys.binding_add_placeholder')}>
                        {(v: string) => {
                          const n = addableNodes.find((node) => node.id === v);
                          return n
                            ? `${n.name} (${capacityText(n.profile_count, n.max_profiles)})`
                            : t('keys.binding_add_placeholder');
                        }}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {addableNodes.map((n) => {
                        const full = isNodeFull(n);
                        return (
                          <SelectItem key={n.id} value={n.id} disabled={full}>
                            {n.name} ({capacityText(n.profile_count, n.max_profiles)})
                          </SelectItem>
                        );
                      })}
                    </SelectContent>
                  </Select>
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    disabled={!addNodeId || bindKey.isPending}
                    onClick={() => void handleAddNode()}
                  >
                    <Plus />
                    <span className="sr-only">{t('common.add')}</span>
                  </Button>
                </div>
              )}
            </Section>

            <div {...enter(2)}>
              <KeyStatsSection keyId={key.id} hasTelemtNode={hasTelemtNode} />
            </div>

            <div {...enter(3)}>
              <SubscriptionLinkSection
                key={key.id}
                keyId={key.id}
                subscriptionActive={key.subscription_active}
                isWriter={isWriter}
                locked={locked}
              />
            </div>

            {isWriter && !revoked && (
              <Section title={t('keys.danger_zone')} {...enter(4)}>
                <div className="flex flex-wrap gap-2">
                  {key.type === 'SHARED' && (
                    <Button type="button" variant="destructive" size="sm" onClick={() => setRotateOpen(true)}>
                      {t('keys.action_rotate')}
                    </Button>
                  )}
                  <Button type="button" variant="destructive" size="sm" onClick={() => setRevokeOpen(true)}>
                    {t('keys.action_revoke')}
                  </Button>
                </div>
              </Section>
            )}
          </div>
        )}

        {key && (
          <>
            <ConfirmDialog
              open={rotateOpen}
              onOpenChange={setRotateOpen}
              title={t('keys.rotate_confirm_title', { label: key.label })}
              description={t('keys.rotate_confirm_description')}
              confirmLabel={t('keys.action_rotate')}
              onConfirm={handleRotate}
            />
            <ConfirmDialog
              open={revokeOpen}
              onOpenChange={setRevokeOpen}
              title={t('keys.revoke_confirm_title', { label: key.label })}
              description={key.type === 'SHARED' ? t('keys.revoke_confirm_shared') : t('keys.revoke_confirm_personal')}
              destructive
              confirmLabel={t('keys.action_revoke')}
              onConfirm={handleRevoke}
            />
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
