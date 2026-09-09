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
  { name: 'max_unique_ips', step: '1', placeholder: '3', max: MAX_TELEMT_COUNTER },
  { name: 'max_tcp_conns', step: '1', placeholder: '64', max: MAX_TELEMT_COUNTER },
] as const satisfies readonly { name: keyof TelemtLimitsForm; step: string; placeholder: string; max?: number }[];

const NUMBER_FIELD_CLASS =
  'mono text-mono [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none';

interface TelemtLimitsFieldsProps {
  value: TelemtLimitsForm;
  onChange: (next: TelemtLimitsForm) => void;
  errors?: Partial<Record<keyof TelemtLimitsForm, string>>;
  /** True when no telemt node is in play - the fields are shown, but there is nothing to enforce them. */
  disabled?: boolean;
  className?: string;
}

function splitUnit(label: string): { name: string; unit: string | null } {
  const comma = label.lastIndexOf(',');
  if (comma <= 0) return { name: label, unit: null };
  return { name: label.slice(0, comma), unit: label.slice(comma + 1).trim() || null };
}

export function TelemtLimitsFields({ value, onChange, errors, disabled, className }: TelemtLimitsFieldsProps) {
  const { t } = useTranslation();

  return (
    <div className={cn('space-y-3', className)}>
      {disabled ? (
        <p className="text-label text-mute">{t('keys.telemt_limits_unavailable')}</p>
      ) : (
        <p className="text-label text-mute">{t('keys.telemt_limits_hint')}</p>
      )}

      <div className={cn('grid grid-cols-1 gap-x-4 gap-y-3 sm:grid-cols-2', disabled && 'opacity-55')}>
        {TELEMT_LIMIT_FIELDS.map((field) => {
          const label = t(`keys.telemt_limit_${field.name}`);
          const { name, unit } = splitUnit(label);
          return (
            <div key={field.name} className="space-y-2">
              <Label htmlFor={`telemt-limit-${field.name}`} className="font-normal text-mute">
                {name}
              </Label>
              <div className="relative">
                <Input
                  id={`telemt-limit-${field.name}`}
                  type="number"
                  min={0}
                  max={'max' in field ? field.max : undefined}
                  step={field.step}
                  inputMode="decimal"
                  placeholder={field.placeholder}
                  aria-label={label}
                  className={cn(NUMBER_FIELD_CLASS, unit && 'pr-16')}
                  disabled={disabled}
                  value={value[field.name]}
                  onChange={(e) => onChange({ ...value, [field.name]: e.target.value })}
                  aria-invalid={!!errors?.[field.name]}
                />
                {unit && (
                  <span
                    aria-hidden="true"
                    className="pointer-events-none absolute inset-y-0 right-0 flex items-center border-l border-hairline px-2 text-label text-mute"
                  >
                    {unit}
                  </span>
                )}
              </div>
              {errors?.[field.name] && <p className="text-label text-destructive">{errors[field.name]}</p>}
            </div>
          );
        })}
      </div>
    </div>
  );
}
