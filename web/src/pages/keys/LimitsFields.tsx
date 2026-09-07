import { useTranslation } from 'react-i18next';

import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';

import type { ProfileLimits } from '@/api/types';

export const LIMIT_FIELD_NAMES = [
  'max_sessions',
  'max_streams',
  'max_backend_dials_in_flight',
  'new_sessions_per_minute',
  'new_sessions_burst',
  'new_streams_per_minute',
  'new_streams_burst',
  'max_streams_per_session',
  'max_pending_per_session',
] as const;

export type LimitFieldName = (typeof LIMIT_FIELD_NAMES)[number];

export const ZERO_LIMITS: Required<ProfileLimits> = {
  max_sessions: 0,
  max_streams: 0,
  max_backend_dials_in_flight: 0,
  new_sessions_per_minute: 0,
  new_sessions_burst: 0,
  new_streams_per_minute: 0,
  new_streams_burst: 0,
  max_streams_per_session: 0,
  max_pending_per_session: 0,
};

interface LimitsFieldsProps {
  value: ProfileLimits;
  onChange: (next: ProfileLimits) => void;
  errors?: Partial<Record<LimitFieldName, string>>;
  className?: string;
}

/**
 * Advanced per-key relay limits (see internal/domain.ProfileLimits): plain
 * non-negative integer inputs, zero meaning "inherit the node's global
 * default". Shared between CreateKeyDialog and KeyDetailDrawer via a
 * value/onChange contract so each caller can wire it through its own
 * react-hook-form `Controller` without sharing form-generic typing.
 */
export function LimitsFields({ value, onChange, errors, className }: LimitsFieldsProps) {
  const { t } = useTranslation();

  const setField = (name: LimitFieldName, raw: string) => {
    const n = raw.trim() === '' ? 0 : Math.max(0, Math.trunc(Number(raw)));
    onChange({ ...value, [name]: Number.isFinite(n) ? n : 0 });
  };

  return (
    <div className={cn('grid grid-cols-1 gap-x-4 gap-y-3 sm:grid-cols-2', className)}>
      {LIMIT_FIELD_NAMES.map((name) => (
        <div key={name} className="space-y-2">
          <Label htmlFor={`limit-${name}`} className="font-normal text-mute">
            {t(`keys.limit_${name}`)}
          </Label>
          <Input
            id={`limit-${name}`}
            type="number"
            min={0}
            step={1}
            inputMode="numeric"
            placeholder="0"
            className="mono text-mono"
            value={value[name] ?? 0}
            onChange={(e) => setField(name, e.target.value)}
            aria-invalid={!!errors?.[name]}
          />
          {errors?.[name] && <p className="text-label text-destructive">{errors[name]}</p>}
        </div>
      ))}
    </div>
  );
}
