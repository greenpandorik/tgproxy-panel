import { useTranslation } from 'react-i18next';

import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';
import { cn } from '@/lib/utils';

import type { ReactNode } from 'react';

// Panels arrive with the entrance, staggered.
export function Arriving({ index = 0, className, children }: { index?: number; className?: string; children: ReactNode }) {
  return (
    <div className={cn(ENTER_CLASS, className)} style={enterDelay(index)}>
      {children}
    </div>
  );
}

/** A settings tab at full width: its panels side by side when there is room, one under another when not. */
export function FormColumns({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div className="@container min-w-0">
      <div className={cn('grid items-start gap-4 @min-[56rem]:grid-cols-2', className)}>{children}</div>
    </div>
  );
}

/**
 * The strip under a form with its buttons. With `dirty` it says the changes are not saved yet, and
 * it stays at the bottom of the screen while the form is longer than the screen.
 */
export function FormFooter({ note, dirty, children }: { note?: ReactNode; dirty?: boolean; children?: ReactNode }) {
  const { t } = useTranslation();
  return (
    <div className="sticky bottom-0 z-10 flex flex-wrap items-center gap-3 rounded-surface border border-hairline-strong bg-card px-4 py-3">
      {dirty ? (
        <p role="status" className="flex min-w-0 items-center gap-2 text-label text-foreground">
          <span className="size-[7px] shrink-0 rounded-pill bg-warn" aria-hidden="true" />
          {t('settings.unsaved')}
        </p>
      ) : (
        note && <p className="min-w-0 text-label text-mute">{note}</p>
      )}
      {children && <div className="ml-auto flex flex-wrap items-center gap-2">{children}</div>}
    </div>
  );
}
