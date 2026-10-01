import { ChevronRight } from 'lucide-react';

import { cn } from '@/lib/utils';

/** The arrow at the end of a row that opens something; it lights up with its row. */
export function RowChevron({ className }: { className?: string }) {
  return (
    <ChevronRight
      aria-hidden="true"
      className={cn('size-4 shrink-0 text-mute transition-colors group-hover/row:text-primary', className)}
    />
  );
}
