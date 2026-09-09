import { Button as ButtonPrimitive } from '@base-ui/react/button';
import { cva, type VariantProps } from 'class-variance-authority';

import { cn } from '@/lib/utils';

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center gap-1.5 rounded-control border border-transparent text-body font-medium whitespace-nowrap transition-[background-color,border-color,color,scale] outline-none select-none active:scale-[0.985] focus-visible:ring-2 focus-visible:ring-ring/70 focus-visible:ring-offset-1 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-45 aria-invalid:border-destructive [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:ring-2 hover:ring-primary/30',
        outline:
          'border-hairline-strong bg-surface text-foreground hover:bg-elevated aria-expanded:bg-elevated data-popup-open:bg-elevated',
        secondary: 'bg-elevated text-foreground hover:bg-elevated/70 aria-expanded:bg-elevated',
        ghost:
          'text-muted-foreground hover:bg-elevated hover:text-foreground aria-expanded:bg-elevated aria-expanded:text-foreground',
        // Red text on a hairline.
        destructive:
          'border-hairline-strong bg-transparent text-destructive hover:border-destructive/40 hover:bg-elevated focus-visible:ring-destructive/60',
        'destructive-solid':
          'bg-destructive-solid text-destructive-foreground hover:bg-destructive-solid-hover focus-visible:ring-destructive/60',
        /** A link is prose, so it takes the brand hue at text weight (see --brand-ink). */
        link: 'text-brand-ink underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-[var(--control-height)] px-4',
        xs: 'h-6 gap-1 px-1.5 text-label [&_svg:not([class*=size-])]:size-3',
        sm: 'h-8 gap-1.5 px-3 text-label [&_svg:not([class*=size-])]:size-3.5',
        lg: 'h-10 px-4',
        icon: 'size-10',
        'icon-xs': 'size-6 [&_svg:not([class*=size-])]:size-3',
        'icon-sm': 'size-9 [&_svg:not([class*=size-])]:size-3.5',
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
