import { describe, expect, it } from 'vitest';

import { cn } from './utils';

describe('cn', () => {
  it('keeps a colour and a role size together', () => {
    expect(cn('bg-foreground text-background', 'text-label')).toBe('bg-foreground text-background text-label');
    expect(cn('text-mute', 'text-body')).toBe('text-mute text-body');
    expect(cn('text-display text-foreground')).toBe('text-display text-foreground');
  });

  it('still lets one role size win over another', () => {
    expect(cn('text-body', 'text-label')).toBe('text-label');
    expect(cn('text-micro text-display')).toBe('text-display');
  });

  it('still lets one colour win over another', () => {
    expect(cn('text-mute', 'text-foreground')).toBe('text-foreground');
  });

  it('does not confuse a role size with a Tailwind size', () => {
    expect(cn('text-sm', 'text-body')).toBe('text-body');
    expect(cn('text-body', 'text-sm')).toBe('text-sm');
  });
});
