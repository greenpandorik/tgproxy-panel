import type { LucideIcon } from 'lucide-react';
import type { CSSProperties, ReactNode } from 'react';

import { cn } from '@/lib/utils';

interface EmptyStateProps {
  /** The subject's own glyph, on a tinted plate above the text. */
  icon?: LucideIcon;
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
}

// Empty is not an error and not a mood: one muted line saying what is not here, and the button that fixes it.
export function EmptyState({ icon: Icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center gap-3 rounded-surface border border-hairline-strong py-14 text-center',
        className,
      )}
    >
      {Icon && (
        <span
          className="tgwp-tone-tint flex size-9 items-center justify-center rounded-control border"
          style={{ '--tone': 'var(--mute)' } as CSSProperties}
          aria-hidden="true"
        >
          <Icon size={18} strokeWidth={1.8} />
        </span>
      )}
      <div className="space-y-1 px-6">
        <p className="text-body text-muted-foreground">{title}</p>
        {description && <p className="text-label text-mute">{description}</p>}
      </div>
      {action}
    </div>
  );
}

// The same fact one level in: this *panel* has nothing to list.
export function PanelEmpty({ children, className }: { children: ReactNode; className?: string }) {
  return <p className={cn('px-6 py-10 text-center text-body text-mute', className)}>{children}</p>;
}
