import { mergeProps } from '@base-ui/react/merge-props';
import { useRender } from '@base-ui/react/use-render';
import { cva, type VariantProps } from 'class-variance-authority';

import { cn } from '@/lib/utils';

// Tag, not a pill: micro-sized mono on a hairline rectangle.
const badgeVariants = cva(
  'group/badge mono inline-flex h-5 w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-control border px-1.5 text-micro whitespace-nowrap transition-colors focus-visible:ring-2 focus-visible:ring-ring/50 [&>svg]:pointer-events-none [&>svg]:size-3!',
  {
    variants: {
      variant: {
        default: 'border-hairline-strong bg-transparent text-muted-foreground',
        secondary: 'border-transparent bg-elevated text-muted-foreground',
        destructive: 'border-destructive/35 bg-transparent text-destructive',
        /** A fact that is not wrong yet but wants noticing: config not pushed, cert near expiry. */
        warn: 'border-warn/35 bg-transparent text-warn',
        outline: 'border-hairline-strong bg-transparent text-foreground',
        ghost: 'border-transparent bg-transparent text-mute',
        link: 'border-transparent text-brand-ink underline-offset-4 hover:underline',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  },
);

function Badge({
  className,
  variant = 'default',
  render,
  ...props
}: useRender.ComponentProps<'span'> & VariantProps<typeof badgeVariants>) {
  return useRender({
    defaultTagName: 'span',
    props: mergeProps<'span'>({ className: cn(badgeVariants({ variant }), className) }, props),
    render,
    state: { slot: 'badge', variant },
  });
}

export { Badge, badgeVariants };
