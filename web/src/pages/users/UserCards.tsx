import { useTranslation } from 'react-i18next';
import { Controller } from 'react-hook-form';

import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { HelpButton } from '@/help';
import { AdvancedSettings } from '@/components/common/AdvancedSettings';
import { cn } from '@/lib/utils';

import { LimitsFields } from './LimitsFields';
import { NodeCapacityList } from './NodeCapacityList';
import { TelemtLimitsFields } from './TelemtLimitsFields';
import { TransportField } from './TransportField';
import { extendExpiry, toSlug } from './userForm';

import type { LimitFieldName } from './LimitsFields';
import type { TransportScope } from './transport';
import type { TelemtErrors, UserFormValues } from './userForm';
import type { Node } from '@/api/types';
import type { LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';
import type { Control, FieldErrors, UseFormRegister, UseFormSetValue } from 'react-hook-form';

export function UserCard({
  icon: Icon,
  title,
  actions,
  className,
  children,
}: {
  icon?: LucideIcon;
  title: string;
  actions?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section className={cn('min-w-0 rounded-surface border border-hairline bg-card', className)}>
      <header className="flex items-center gap-2 border-b border-hairline px-4 py-3">
        {Icon && <Icon size={16} strokeWidth={1.8} className="shrink-0 text-mute" aria-hidden="true" />}
        <h3 className="text-body font-semibold">{title}</h3>
        {actions && <div className="ml-auto flex items-center gap-1">{actions}</div>}
      </header>
      <div className="space-y-4 p-4">{children}</div>
    </section>
  );
}

function FieldError({ children }: { children?: ReactNode }) {
  if (!children) return null;
  return <p className="text-label text-destructive">{children}</p>;
}

interface FormBits {
  control: Control<UserFormValues>;
  register: UseFormRegister<UserFormValues>;
  errors: FieldErrors<UserFormValues>;
  values: UserFormValues;
  locked?: boolean;
}

export function AboutFields({ control, register, errors, values, locked, subscriptionBase }: FormBits & { subscriptionBase: string }) {
  const { t } = useTranslation();
  const batch = values.mode === 'batch';
  return (
    <>
      {batch ? (
        <div className="grid grid-cols-1 gap-x-4 gap-y-3 sm:grid-cols-[1fr_auto]">
          <div className="space-y-2">
            <Label htmlFor="user-prefix">{t('keys.field_prefix')}</Label>
            <Input id="user-prefix" autoFocus placeholder="vip" {...register('prefix')} aria-invalid={!!errors.prefix} />
            <p className="text-label text-mute">
              {t('users.prefix_hint', { prefix: values.prefix.trim() || 'vip' })}
            </p>
            <FieldError>{errors.prefix && t('common.required')}</FieldError>
          </div>
          <div className="space-y-2">
            <Label htmlFor="user-count">{t('keys.field_count')}</Label>
            <Input
              id="user-count"
              type="number"
              min={1}
              max={100}
              className="mono w-24 text-mono"
              {...register('count')}
              aria-invalid={!!errors.count}
            />
            <FieldError>{errors.count && t('keys.validation_count')}</FieldError>
          </div>
        </div>
      ) : (
        <div className="space-y-2">
          <Label htmlFor="user-label">{t('users.field_name')}</Label>
          <Input id="user-label" autoFocus={values.creating} disabled={locked} {...register('label')} aria-invalid={!!errors.label} />
          <FieldError>{errors.label && t('common.required')}</FieldError>
        </div>
      )}

      {!batch && (
        <div className="space-y-2">
          <Label htmlFor="user-contact">
            {t('users.field_contact')} <span className="font-normal text-mute">({t('keys.field_optional')})</span>
          </Label>
          <Input id="user-contact" disabled={locked} placeholder={t('users.field_contact_placeholder')} {...register('owner_label')} />
        </div>
      )}

      {values.mode === 'shared' && (
        <div className="space-y-2">
          <Label htmlFor="user-slug">
            {t('users.field_slug')} <span className="font-normal text-mute">({t('keys.field_optional')})</span>
          </Label>
          <div className="flex min-w-0 items-center gap-1">
            <span className="mono hidden shrink-0 text-mono text-mute sm:inline">{subscriptionBase}/s/</span>
            <Controller
              control={control}
              name="sub_slug"
              render={({ field }) => (
                <Input
                  id="user-slug"
                  className="mono text-mono"
                  placeholder="team"
                  disabled={locked}
                  value={field.value}
                  onChange={(e) => field.onChange(toSlug(e.target.value))}
                  onBlur={field.onBlur}
                  aria-invalid={!!errors.sub_slug}
                />
              )}
            />
          </div>
          {errors.sub_slug ? (
            <FieldError>{errors.sub_slug.message === 'slug' ? t('users.validation_slug') : errors.sub_slug.message}</FieldError>
          ) : (
            <p className="text-label text-mute">{t('users.field_slug_hint')}</p>
          )}
        </div>
      )}

      <div className="space-y-2">
        <Label htmlFor="user-note">
          {t('keys.field_note')} <span className="font-normal text-mute">({t('keys.field_optional')})</span>
        </Label>
        <Textarea id="user-note" rows={2} disabled={locked} {...register('note')} />
      </div>
    </>
  );
}

export function AccessFields({
  control,
  register,
  errors,
  values,
  locked,
  setValue,
  nodes,
  scope,
}: FormBits & { setValue: UseFormSetValue<UserFormValues>; nodes: Node[]; scope: TransportScope }) {
  const { t } = useTranslation();
  const setExpiry = (next: string) => {
    setValue('no_expiry', false, { shouldDirty: true });
    setValue('expires_at', next, { shouldDirty: true, shouldValidate: true });
  };
  return (
    <>
      <div className="space-y-2">
        <Label htmlFor="user-expires">{t('users.field_expires')}</Label>
        <div className="flex flex-wrap items-center gap-2">
          <Input
            id="user-expires"
            type="datetime-local"
            className="mono w-fit text-mono"
            disabled={locked || values.no_expiry}
            {...register('expires_at')}
            aria-invalid={!!errors.expires_at}
          />
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
        {!locked && (
          <div className="flex flex-wrap gap-1.5">
            {(['1m', '3m', '1y'] as const).map((step) => (
              <Button
                key={step}
                type="button"
                variant="outline"
                size="xs"
                onClick={() => setExpiry(extendExpiry(values.no_expiry ? '' : values.expires_at, step))}
              >
                {t(`users.extend_${step}`)}
              </Button>
            ))}
          </div>
        )}
        <FieldError>
          {errors.expires_at &&
            t(errors.expires_at.message === 'future' ? 'keys.validation_expires_future' : 'common.required')}
        </FieldError>
      </div>

      <div className="space-y-2">
        <Label>{t('keys.field_nodes')}</Label>
        <Controller
          control={control}
          name="node_ids"
          render={({ field }) => (
            <NodeCapacityList
              nodes={nodes}
              selectedIds={field.value}
              onChange={locked ? () => undefined : field.onChange}
              className={cn(locked && 'pointer-events-none opacity-60')}
            />
          )}
        />
        <FieldError>{errors.node_ids && t('keys.validation_node_ids')}</FieldError>
      </div>

      {!locked && scope !== 'telemt' && (
        <Controller
          control={control}
          name="carrier_mode"
          render={({ field }) => <TransportField scope={scope} value={field.value} onChange={field.onChange} />}
        />
      )}
    </>
  );
}

export function LimitsCardBody({
  control,
  errors,
  locked,
  scope,
}: Pick<FormBits, 'control' | 'errors' | 'locked'> & { scope: TransportScope }) {
  const { t } = useTranslation();
  const telemt = scope === 'telemt' || scope === 'mixed';
  const tproxy = scope === 'tproxy' || scope === 'mixed';

  const limitsError = (name: LimitFieldName): string | undefined => {
    const msg = errors.limits?.[name]?.message;
    return msg ? t(`keys.validation_${msg}`) : undefined;
  };
  const telemtErrors = (): TelemtErrors => {
    const out: TelemtErrors = {};
    for (const [name, err] of Object.entries(errors.telemt_limits ?? {})) {
      const msg = (err as { message?: string } | undefined)?.message;
      if (msg) out[name as keyof TelemtErrors] = t(`keys.validation_${msg}`);
    }
    return out;
  };

  return (
    <>
      {scope === 'none' ? (
        <p className="text-label text-mute">{t('users.limits_pick_servers')}</p>
      ) : (
        <Controller
          control={control}
          name="telemt_limits"
          render={({ field }) => (
            <TelemtLimitsFields value={field.value} onChange={field.onChange} disabled={locked || !telemt} errors={telemtErrors()} />
          )}
        />
      )}
      {tproxy && (
        <AdvancedSettings label={t('keys.field_limits_toggle')} className="border-t border-hairline pt-3">
          <div className="flex items-center gap-2">
            <p className="text-label text-mute">{t('keys.legacy_limits_hint')}</p>
            <HelpButton topic="keys.limits" className="-my-1" />
          </div>
          <Controller
            control={control}
            name="limits"
            render={({ field }) => (
              <LimitsFields
                value={field.value}
                onChange={locked ? () => undefined : field.onChange}
                telemtOnly={false}
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
        </AdvancedSettings>
      )}
    </>
  );
}
