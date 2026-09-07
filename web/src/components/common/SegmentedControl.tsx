import { cn } from '@/lib/utils';

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
}

interface SegmentedControlProps<T extends string> {
  value: T;
  options: SegmentedOption<T>[];
  onChange: (value: T) => void;
  /** Names the group for screen readers - the buttons alone only say "1h", "7d". */
  label: string;
  className?: string;
}

/**
 * A few mutually exclusive choices in one hairline group, the chosen one filled
 * with --bg-3.
 *
 * A segmented group says "these are the same kind of thing and you get one of
 * them"; separate buttons would not, and an underlined tab row would read as page
 * navigation. Used for the time windows on the monitoring page and in the key
 * drawer, which is why it lives here rather than in either of them.
 */
export function SegmentedControl<T extends string>({ value, options, onChange, label, className }: SegmentedControlProps<T>) {
  return (
    <div role="radiogroup" aria-label={label} className={cn('inline-flex rounded-control border border-hairline-strong p-0.5', className)}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="radio"
          aria-checked={value === option.value}
          onClick={() => onChange(option.value)}
          className={cn(
            'rounded-control px-2.5 py-1 text-label transition-[background-color,color,scale] outline-none active:scale-[0.985] focus-visible:ring-2 focus-visible:ring-ring/70',
            value === option.value ? 'bg-elevated text-foreground' : 'text-mute hover:text-foreground',
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
