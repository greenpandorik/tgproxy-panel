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

/*
 * The unit sits inside the field, at its trailing edge, which is exactly where
 * a number input draws its native spin buttons - so on the three fields that
 * carry a unit the spinners would land underneath it. They are switched off for
 * the whole grid rather than for three of five boxes, because a row of fields
 * where only some of them step on hover reads as a bug. Typing and the arrow
 * keys are untouched; these are values an operator types, not nudges.
 */
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

/**
 * Splits a field's label into the thing being limited and the unit it is
 * measured in. Three of the five labels are written "Traffic quota, GB"; the
 * two counters carry no unit and come back with `unit` null.
 *
 * The unit is separated so it can be rendered inside the field rather than read
 * as prose in the label: an operator typing 50 into a box that already says GB
 * never has to check which of the five boxes wanted gigabytes. The input keeps
 * the whole label as its accessible name, so nothing is lost to a screen reader.
 */
function splitUnit(label: string): { name: string; unit: string | null } {
  const comma = label.lastIndexOf(',');
  if (comma <= 0) return { name: label, unit: null };
  return { name: label.slice(0, comma), unit: label.slice(comma + 1).trim() || null };
}

/**
 * Per-key limits telemt enforces on the node: a traffic quota, an up/down speed
 * cap, and ceilings on unique addresses and open connections.
 *
 * The units are the ones an operator says out loud - gigabytes and megabits per
 * second - and they sit at the trailing edge of the field itself, so the number
 * being typed and the unit it is in are one object rather than a caption and a
 * box. An empty field is "no limit"; that is stated once above the grid instead
 * of five times in five placeholders.
 *
 * When the key is bound only to tproxy nodes the whole block is disabled and says
 * so in the engine's own vocabulary: the limits are a telemt feature, not a
 * setting that failed to save.
 */
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
