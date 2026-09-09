import { ChevronDown, SlidersHorizontal } from 'lucide-react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { cn } from '@/lib/utils';

import type { ReactNode } from 'react';

interface AdvancedSettingsProps {
  children: ReactNode;
  /** Overrides both wordings at once, for a section that needs its own noun. */
  label?: string;
  /** Open on mount. Nothing is remembered between pages or visits. */
  defaultOpen?: boolean;
  className?: string;
  contentClassName?: string;
}

/** The panel's one control for hiding technical options from everyday work. */
export function AdvancedSettings({ children, label, defaultOpen = false, className, contentClassName }: AdvancedSettingsProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(defaultOpen);
  const contentId = useId();

  return (
    <div data-slot="advanced-settings" className={cn('flex flex-col gap-3', className)}>
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        aria-controls={contentId}
        className="inline-flex w-fit items-center gap-2 rounded-control text-label text-mute transition-colors hover:text-foreground"
      >
        <SlidersHorizontal size={14} strokeWidth={1.8} aria-hidden="true" />
        {label ?? t(open ? 'common.advanced_hide' : 'common.advanced_show')}
        <ChevronDown
          size={14}
          strokeWidth={1.8}
          aria-hidden="true"
          className={cn('duration-fast transition-transform', open && 'rotate-180')}
        />
      </button>
      <div id={contentId} hidden={!open} className={cn('flex flex-col gap-4', contentClassName)}>
        {children}
      </div>
    </div>
  );
}
