import { ChevronRight } from 'lucide-react';
import { Fragment } from 'react';
import { useSearchParams } from 'react-router-dom';
import { cn } from '@/lib/utils';

export type SectionItem = { value: string; label: string; description?: string; group?: string };

/** URL-backed sections preserve Back, refresh and direct links without discarding other filters. */
export function useSection(values: readonly string[], fallback: string, key = 'section') {
  const [params, setParams] = useSearchParams();
  const raw = params.get(key);
  const section = raw && values.includes(raw) ? raw : fallback;
  const setSection = (value: string) => {
    if (!values.includes(value)) return;
    setParams((current) => {
      const next = new URLSearchParams(current);
      next.set(key, value);
      return next;
    });
  };
  return [section, setSection] as const;
}

type SectionProps = {
  label: string;
  items: SectionItem[];
  value: string;
  onChange: (value: string) => void;
};

const startsGroup = (items: SectionItem[], index: number) =>
  Boolean(items[index].group) && items[index].group !== items[index - 1]?.group;

/** The narrow-layout form of either navigation. A group of one gets no heading of its own. */
function SectionSelect({ label, items, value, onChange, className }: SectionProps & { className?: string }) {
  const option = (item: SectionItem) => (
    <option key={item.value} value={item.value}>
      {item.label}
    </option>
  );
  return (
    <label className={cn('block space-y-2', className)}>
      <span className="text-label text-mute">{label}</span>
      <select className="ops-select" value={value} onChange={(e) => onChange(e.target.value)}>
        {items.map((item, index) => {
          if (!item.group) return option(item);
          if (!startsGroup(items, index)) return null;
          const members = items.filter((other) => other.group === item.group);
          if (members.length === 1) return option(item);
          return (
            <optgroup key={item.group} label={item.group}>
              {members.map(option)}
            </optgroup>
          );
        })}
      </select>
    </label>
  );
}

export function SectionNav({ label, items, value, onChange }: SectionProps) {
  return (
    <nav aria-label={label} className="section-nav min-w-0 lg:w-56 lg:shrink-0">
      <SectionSelect label={label} items={items} value={value} onChange={onChange} className="lg:hidden" />
      <div className="hidden gap-1 rounded-surface border border-hairline-strong bg-card p-2 lg:grid lg:grid-cols-1">
        {items.map((item, index) => (
          <div key={item.value} className="min-w-0">
            {startsGroup(items, index) && <p className="px-3 pb-2 pt-3 text-label font-medium text-mute">{item.group}</p>}
            <button
              type="button"
              aria-current={value === item.value ? 'page' : undefined}
              onClick={() => onChange(item.value)}
              className={cn(
                'flex min-h-11 w-full items-center gap-2 rounded-control border px-3 py-2.5 text-left transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
                value === item.value
                  ? 'border-hairline-strong bg-elevated text-foreground'
                  : 'border-transparent text-mute hover:border-hairline hover:bg-elevated/60 hover:text-foreground',
              )}
            >
              <span className="min-w-0 flex-1">
                <span className="block text-body font-medium">{item.label}</span>
                {item.description && <span className="mt-0.5 block text-micro text-mute">{item.description}</span>}
              </span>
              <ChevronRight className="size-3.5 shrink-0" aria-hidden="true" />
            </button>
          </div>
        ))}
      </div>
    </nav>
  );
}

/**
 * The same sections as a row of tabs above the content; groups are set apart by a rule.
 * The row appears only when its own box is wide enough for every tab, so a narrow window or an
 * expanded sidebar gets the select instead of tabs pushed out of sight.
 */
export function SectionTabs({ label, items, value, onChange }: SectionProps) {
  return (
    <nav aria-label={label} className="@container min-w-0">
      <SectionSelect label={label} items={items} value={value} onChange={onChange} className="@min-[52rem]:hidden" />
      <div className="hidden border-b border-hairline @min-[52rem]:block">
        <div className="-mb-px flex items-center gap-4 overflow-x-auto [scrollbar-width:thin]">
          {items.map((item, index) => (
            <Fragment key={item.value}>
              {index > 0 && startsGroup(items, index) && (
                <span data-testid="section-tabs-divider" aria-hidden="true" className="h-4 w-px shrink-0 bg-hairline-strong" />
              )}
              <button
                type="button"
                aria-current={value === item.value ? 'page' : undefined}
                onClick={() => onChange(item.value)}
                className={cn(
                  'relative inline-flex h-10 shrink-0 items-center whitespace-nowrap border-b-2 px-0.5 text-body font-medium transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
                  value === item.value
                    ? 'border-foreground text-foreground'
                    : 'border-transparent text-mute hover:text-foreground',
                )}
              >
                {item.label}
              </button>
            </Fragment>
          ))}
        </div>
      </div>
    </nav>
  );
}
