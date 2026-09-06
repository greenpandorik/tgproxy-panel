import { useTranslation } from 'react-i18next';

import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';

import { MAX_TELEMT_COUNTER } from '@/lib/units';

import type { TelemtLimitsForm } from '@/lib/units';

/** The five limits, in the order an operator sets them: how much, how fast, how many. */
export const TELEMT_LIMIT_FIELDS = [
  { name: 'quota_gb', step: 'any', placeholder: '50' },
  { name: 'rate_up_mbit', step: 'any', placeholder: '20' },
  { name: 'rate_down_mbit', step: 'any', placeholder: '100' },
  // The two counters carry the backend's ceiling as a real max: they travel as 32-bit
  // unsigned integers, and a value past 2^32-1 would wrap into a tiny limit that locks the
  // key out rather than into "unlimited".
  { name: 'max_unique_ips', step: '1', placeholder: '3', max: MAX_TELEMT_COUNTER },
  { name: 'max_tcp_conns', step: '1', placeholder: '64', max: MAX_TELEMT_COUNTER },
] as const satisfies readonly { name: keyof TelemtLimitsForm; step: string; placeholder: string; max?: number }[];

interface TelemtLimitsFieldsProps {
  value: TelemtLimitsForm;
  onChange: (next: TelemtLimitsForm) => void;
  errors?: Partial<Record<keyof TelemtLimitsForm, string>>;
  /** True when no telemt node is in play - the fields are shown, but there is nothing to enforce them. */
  disabled?: boolean;
  className?: string;
}

/**
 * Per-key limits telemt enforces on the node: a traffic quota, an up/down speed
 * cap, and ceilings on unique addresses and open connections.
 *
 * The units are the ones an operator says out loud - gigabytes and megabits per
 * second - and live in the label rather than in a decoration inside the field, so
 * the input stays a plain number and the column of figures still lines up. An
 * empty field is "no limit"; that is stated once above the grid instead of five
 * times in five placeholders.
 *
 * When the key is bound only to tproxy nodes the whole block is disabled and says
 * so in the engine's own vocabulary: the limits are a telemt feature, not a
 * setting that failed to save.
 */
export function TelemtLimitsFields({ value, onChange, errors, disabled, className }: TelemtLimitsFieldsProps) {
  const { t } = useTranslation();

  return (
    <div className={cn('space-y-2.5', className)}>
      {disabled ? (
        <p className="mono text-xs text-dim">{t('keys.telemt_limits_unavailable')}</p>
      ) : (
        <p className="text-xs text-mute">{t('keys.telemt_limits_hint')}</p>
      )}

      <div className={cn('grid grid-cols-1 gap-x-4 gap-y-2.5 sm:grid-cols-2', disabled && 'opacity-55')}>
        {TELEMT_LIMIT_FIELDS.map((field) => (
          <div key={field.name} className="space-y-1">
            <Label htmlFor={`telemt-limit-${field.name}`} className="text-xs font-normal text-mute">
              {t(`keys.telemt_limit_${field.name}`)}
            </Label>
            <Input
              id={`telemt-limit-${field.name}`}
              type="number"
              min={0}
              max={'max' in field ? field.max : undefined}
              step={field.step}
              inputMode="decimal"
              placeholder={field.placeholder}
              className="mono h-7 text-xs"
              disabled={disabled}
              value={value[field.name]}
              onChange={(e) => onChange({ ...value, [field.name]: e.target.value })}
              aria-invalid={!!errors?.[field.name]}
            />
            {errors?.[field.name] && <p className="text-xs text-destructive">{errors[field.name]}</p>}
          </div>
        ))}
      </div>
    </div>
  );
}
