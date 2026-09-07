import type { LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';

import { cn } from '@/lib/utils';

interface EmptyStateProps {
  /** Kept for call-site compatibility; the v2 empty state is text + action only. */
  icon?: LucideIcon;
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
}

/**
 * Empty is not an error and not a mood: one muted line saying what is not
 * here, and the button that fixes it. No illustration, no icon medallion -
 * they only push the useful action further down the page.
 */
export function EmptyState({ title, description, action, className }: EmptyStateProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center gap-3 rounded-surface border border-hairline py-14 text-center', className)}>
      <div className="space-y-1 px-6">
        <p className="text-body text-muted-foreground">{title}</p>
        {description && <p className="text-label text-mute">{description}</p>}
      </div>
      {action}
    </div>
  );
}

/**
 * The same fact one level in: this *panel* has nothing to list.
 *
 * It takes no border and no radius - the panel around it already draws both -
 * and one padding, px-6 py-10. The padding is the whole point of the
 * component: the ten in-panel empties in the panel used to be written by hand
 * at py-8, px-4 py-8, px-4 py-6 and px-6 py-10, so two panels stacked in the
 * same column disagreed about how much room "nothing here" needs.
 */
export function PanelEmpty({ children, className }: { children: ReactNode; className?: string }) {
  return <p className={cn('px-6 py-10 text-center text-body text-mute', className)}>{children}</p>;
}
