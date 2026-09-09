import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';

import type { ReactNode } from 'react';

// Panels arrive with the entrance, staggered.
export function Arriving({ index = 0, children }: { index?: number; children: ReactNode }) {
  return (
    <div className={ENTER_CLASS} style={enterDelay(index)}>
      {children}
    </div>
  );
}

export function FormFooter({ note, children }: { note?: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-surface border border-hairline bg-card px-4 py-3">
      {note && <p className="min-w-0 text-label text-mute">{note}</p>}
      {children && <div className="ml-auto flex flex-wrap items-center gap-2">{children}</div>}
    </div>
  );
}
