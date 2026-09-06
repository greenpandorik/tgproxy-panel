import * as React from 'react';

import { cn } from '@/lib/utils';

import { FIELD_CLASS } from './input';

function Textarea({ className, ...props }: React.ComponentProps<'textarea'>) {
  return <textarea data-slot="textarea" className={cn(FIELD_CLASS, 'field-sizing-content min-h-16 py-2', className)} {...props} />;
}

export { Textarea };
