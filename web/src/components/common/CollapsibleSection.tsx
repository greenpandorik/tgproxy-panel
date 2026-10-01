import { Collapsible } from '@base-ui/react/collapsible';
import { ChevronRight } from 'lucide-react';
import { useState } from 'react';

import { cn } from '@/lib/utils';

import type { ReactNode } from 'react';

export type SectionTone = 'neutral' | 'ok' | 'warn' | 'err';

const SUMMARY_TEXT: Record<SectionTone, string> = {
  neutral: 'text-mute',
  ok: 'text-ok',
  warn: 'text-warn',
  err: 'text-err',
};

const SUMMARY_DOT: Partial<Record<SectionTone, string>> = {
  warn: 'bg-warn',
  err: 'bg-err',
};

interface CollapsibleSectionProps {
  title: string;
  /** One line about the contents, shown whether the section is open or closed. */
  summary?: ReactNode;
  tone?: SectionTone;
  /** A control beside the row. It sits outside the toggle, so it works on a closed section. */
  action?: ReactNode;
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** Keeps a closed section's content in the page, so a half-filled form survives closing it. */
  keepMounted?: boolean;
  id?: string;
  className?: string;
  contentClassName?: string;
  children: ReactNode;
}

/**
 * A row that opens to show more: a chevron, a title and a one-line summary, with a hairline above it.
 * Left to itself, a section whose summary is a warning or an error stays open until it is closed by hand.
 */
export function CollapsibleSection({
  title,
  summary,
  tone = 'neutral',
  action,
  defaultOpen,
  open,
  onOpenChange,
  keepMounted,
  id,
  className,
  contentClassName,
  children,
}: CollapsibleSectionProps) {
  const dot = SUMMARY_DOT[tone];
  const [chosen, setChosen] = useState<boolean | null>(null);
  const problem = tone === 'warn' || tone === 'err';
  return (
    <Collapsible.Root
      id={id}
      open={open ?? chosen ?? (!!defaultOpen || problem)}
      onOpenChange={(next) => {
        setChosen(next);
        onOpenChange?.(next);
      }}
      data-slot="collapsible-section"
      data-tone={tone}
      className={cn('border-t border-hairline first:border-t-0', className)}
    >
      <div className={cn('flex items-center gap-3', action && 'pr-(--panel-x)')}>
        <Collapsible.Trigger
          className={cn(
            'group/section flex min-h-13 min-w-0 flex-1 cursor-pointer items-center gap-3 py-3 pl-(--panel-x) text-left transition-colors hover:bg-elevated/60 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring',
            !action && 'pr-(--panel-x)',
          )}
        >
          <ChevronRight
            aria-hidden="true"
            className="size-4 shrink-0 text-mute transition-transform duration-fast group-hover/section:text-foreground group-data-panel-open/section:rotate-90"
          />
          <span className="flex min-w-0 flex-1 flex-col gap-0.5 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
            <span className="text-body font-semibold text-foreground">{title}</span>
            {summary !== undefined && summary !== null && summary !== '' && (
              <span className={cn('inline-flex min-w-0 items-center gap-2 text-label sm:text-right', SUMMARY_TEXT[tone])}>
                {dot && <span className={cn('size-[7px] shrink-0 rounded-pill', dot)} aria-hidden="true" />}
                <span className="min-w-0">{summary}</span>
              </span>
            )}
          </span>
        </Collapsible.Trigger>
        {action}
      </div>
      <Collapsible.Panel
        keepMounted={keepMounted}
        className="h-(--collapsible-panel-height) overflow-hidden transition-[height] duration-base ease-out data-ending-style:h-0 data-starting-style:h-0 [&[hidden]:not([hidden='until-found'])]:hidden"
      >
        <div className={cn('px-(--panel-x) pt-1 pb-5', contentClassName)}>{children}</div>
      </Collapsible.Panel>
    </Collapsible.Root>
  );
}
