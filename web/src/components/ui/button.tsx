import { Button as ButtonPrimitive } from '@base-ui/react/button';
import { cva, type VariantProps } from 'class-variance-authority';

import { cn } from '@/lib/utils';

/*
 * Buttons in visual language v2 (see docs/superpowers/specs/2026-09-05-ui-redesign.md).
 *
 * The primary button is deliberately *not* branded: it is --fg on --bg, so the
 * single highest-contrast element in any view is always "the main action
 * here", no matter which hue the operator set as their brand. Everything else
 * is a hairline outline or nothing at all, and only "destructive" spends
 * colour - as red text on a hairline, with a filled variant reserved for the
 * confirm dialog that actually does the deleting.
 *
 * Every button also presses: active:scale-[0.985] is a sub-pixel squeeze on
 * the `scale` property, so it costs no layout, and it gives the pointer
 * something to feel on a page whose hover states are only colour. It rides
 * the same --dur-fast / --ease-out pair as the colour change, and
 * prefers-reduced-motion zeroes that duration for it in index.css.
 */
const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center gap-1.5 rounded-control border border-transparent text-body font-medium whitespace-nowrap transition-[background-color,border-color,color,scale] outline-none select-none active:scale-[0.985] focus-visible:ring-2 focus-visible:ring-ring/70 focus-visible:ring-offset-1 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-45 aria-invalid:border-destructive [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: 'bg-foreground text-background hover:bg-foreground/85 active:bg-foreground/75',
        outline:
          'border-hairline-strong bg-surface text-foreground hover:bg-elevated aria-expanded:bg-elevated data-popup-open:bg-elevated',
        secondary: 'bg-elevated text-foreground hover:bg-elevated/70 aria-expanded:bg-elevated',
        ghost: 'text-muted-foreground hover:bg-elevated hover:text-foreground aria-expanded:bg-elevated aria-expanded:text-foreground',
        /*
         * Red text on a hairline. The hover is the neutral --bg-3 the other
         * outline buttons use, not a red wash: a 10% red tint under this label
         * takes it to 4.33:1 on a light surface, and the border is already
         * saying "destructive" without spending contrast to do it.
         */
        destructive:
          'border-hairline-strong bg-transparent text-destructive hover:border-destructive/40 hover:bg-elevated focus-visible:ring-destructive/60',
        /**
         * Filled red. Reserved for the confirming button inside a destructive
         * dialog. The fill is --destructive-solid rather than --destructive:
         * the status red is picked to *read* on a surface, and white on it is
         * only 3.76:1, where white on the solid red is 5.14:1 in both themes.
         */
        'destructive-solid':
          'bg-destructive-solid text-destructive-foreground hover:bg-destructive-solid-hover focus-visible:ring-destructive/60',
        /** A link is prose, so it takes the brand hue at text weight (see --brand-ink). */
        link: 'text-brand-ink underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-8 px-2.5',
        xs: 'h-6 gap-1 px-1.5 text-label [&_svg:not([class*=size-])]:size-3',
        sm: 'h-7 gap-1 px-2 text-label [&_svg:not([class*=size-])]:size-3.5',
        lg: 'h-10 px-4',
        icon: 'size-8',
        'icon-xs': 'size-6 [&_svg:not([class*=size-])]:size-3',
        'icon-sm': 'size-7 [&_svg:not([class*=size-])]:size-3.5',
        'icon-lg': 'size-10',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
);

function Button({
  className,
  variant = 'default',
  size = 'default',
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants>) {
  return <ButtonPrimitive data-slot="button" className={cn(buttonVariants({ variant, size, className }))} {...props} />;
}

export { Button, buttonVariants };
