import { cn } from '@/lib/utils';

/*
 * Loading placeholder: 8% of the foreground, per spec - visible, never a grey
 * block. It keeps the 4px scale radius rather than --r-control: a skeleton is
 * neither a control nor a surface, it is the silhouette of the text or cell it
 * stands in for, and a control radius on an h-3 line would draw a pill.
 */
function Skeleton({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot="skeleton" className={cn('animate-pulse rounded-sm bg-foreground/8', className)} {...props} />;
}

export { Skeleton };
