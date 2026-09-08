import { mergeProps } from '@base-ui/react/merge-props';
import { useRender } from '@base-ui/react/use-render';
import { cva, type VariantProps } from 'class-variance-authority';

import { cn } from '@/lib/utils';

/*
 * Tag, not a pill: micro-sized mono on a hairline rectangle. Badges in this
 * panel always carry a machine value (a carrier mode, a job kind, a role, a
 * count), so they are set in the mono face like every other machine value,
 * and they stay unfilled - a filled badge would out-shout the primary button.
 * A chip is a control in the shape lock, so it takes --r-control like the
 * buttons and fields it sits between. It uses text-micro rather than the full
 * .micro role: the size is chrome, but the value inside is a hostname or an
 * id, and uppercasing those would be a lie about the data.
 *
 * There is one geometry here on purpose. Before the phase 8 fix wave ten chips
 * were hand-written beside this component in three of them - h-5 px-2,
 * px-1.5 py-0.5, and px-1.5 with no height at all - on both hairline
 * strengths, so two chips in the same table row were different heights.
 * Anything chip-shaped goes through this file; a new tone is a variant here,
 * not a class string at the call site.
 */
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
