import { cn } from '@/lib/utils';

/** Loading placeholder: 8% of the foreground, per spec - visible, never a grey block. */
function Skeleton({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot="skeleton" className={cn('animate-pulse rounded-sm bg-foreground/8', className)} {...props} />;
}

export { Skeleton };
