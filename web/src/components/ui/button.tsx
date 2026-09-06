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
 */
const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center gap-1.5 rounded-md border border-transparent text-sm font-medium whitespace-nowrap transition-colors outline-none select-none focus-visible:ring-2 focus-visible:ring-ring/70 focus-visible:ring-offset-1 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-45 aria-invalid:border-destructive [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: 'bg-foreground text-background hover:bg-foreground/85 active:bg-foreground/75',
        outline:
          'border-hairline-strong bg-surface text-foreground hover:bg-elevated aria-expanded:bg-elevated data-popup-open:bg-elevated',
        secondary: 'bg-elevated text-foreground hover:bg-elevated/70 aria-expanded:bg-elevated',
        ghost: 'text-muted-foreground hover:bg-elevated hover:text-foreground aria-expanded:bg-elevated aria-expanded:text-foreground',
        destructive:
          'border-hairline-strong bg-transparent text-destructive hover:border-destructive/40 hover:bg-destructive/10 focus-visible:ring-destructive/60',
        /** Filled red. Reserved for the confirming button inside a destructive dialog. */
        'destructive-solid': 'bg-destructive text-destructive-foreground hover:bg-destructive/85 focus-visible:ring-destructive/60',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-8 px-2.5',
        xs: 'h-6 gap-1 px-1.5 text-xs [&_svg:not([class*=size-])]:size-3',
        sm: 'h-7 gap-1 px-2 [&_svg:not([class*=size-])]:size-3.5',
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
