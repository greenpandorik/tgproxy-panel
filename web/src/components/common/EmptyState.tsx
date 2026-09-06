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
    <div className={cn('flex flex-col items-center justify-center gap-3 rounded-lg border border-hairline py-14 text-center', className)}>
      <div className="space-y-1 px-6">
        <p className="text-sm text-muted-foreground">{title}</p>
        {description && <p className="text-xs text-mute">{description}</p>}
      </div>
      {action}
    </div>
  );
}
