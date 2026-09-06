import * as React from 'react';
import { Input as InputPrimitive } from '@base-ui/react/input';

import { cn } from '@/lib/utils';

/**
 * Recessed field: the page ground (--bg) inside a panel (--bg-2), so an input
 * reads as a hole rather than a raised control. Focus is the only place the
 * brand hue appears on a form.
 */
const FIELD_CLASS =
  'w-full min-w-0 rounded-md border border-hairline-strong bg-background px-2.5 text-sm text-foreground transition-colors outline-none placeholder:text-dim focus-visible:border-ring/60 focus-visible:ring-2 focus-visible:ring-ring/35 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive/60 aria-invalid:ring-2 aria-invalid:ring-destructive/25';

function Input({ className, type, ...props }: React.ComponentProps<'input'>) {
  return (
    <InputPrimitive
      type={type}
      data-slot="input"
      className={cn(
        FIELD_CLASS,
        'h-8 py-1 file:inline-flex file:h-6 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground',
        className,
      )}
      {...props}
    />
  );
}

export { Input, FIELD_CLASS };
