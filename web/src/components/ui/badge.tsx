import { mergeProps } from '@base-ui/react/merge-props';
import { useRender } from '@base-ui/react/use-render';
import { cva, type VariantProps } from 'class-variance-authority';

import { cn } from '@/lib/utils';

/*
 * Tag, not a pill: 11px mono on a 4px hairline rectangle. Badges in this panel
 * always carry a machine value (a carrier mode, a job kind, a role, a count),
 * so they are set in the mono face like every other machine value, and they
 * stay unfilled - a filled badge would out-shout the primary button.
 */
const badgeVariants = cva(
  'group/badge mono inline-flex h-5 w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-sm border px-1.5 text-xs whitespace-nowrap transition-colors focus-visible:ring-2 focus-visible:ring-ring/50 [&>svg]:pointer-events-none [&>svg]:size-3!',
  {
    variants: {
      variant: {
        default: 'border-hairline-strong bg-transparent text-muted-foreground',
        secondary: 'border-transparent bg-elevated text-muted-foreground',
        destructive: 'border-destructive/35 bg-transparent text-destructive',
        outline: 'border-hairline-strong bg-transparent text-foreground',
        ghost: 'border-transparent bg-transparent text-dim',
        link: 'border-transparent text-primary underline-offset-4 hover:underline',
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
