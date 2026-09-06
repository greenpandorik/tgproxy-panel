import { describe, expect, it } from 'vitest';

import { bytesAxisFormatter } from './chartTheme';

describe('bytesAxisFormatter', () => {
  it('prints every tick of an axis in the unit its maximum sets', () => {
    const tick = bytesAxisFormatter(3 * 1024 ** 3);
    expect(tick(3 * 1024 ** 3)).toBe('3.0 GB');
    expect(tick(700 * 1024 ** 2)).toBe('0.7 GB');
    expect(tick(0)).toBe('0 GB');
  });

  it('drops the decimal once the figure is big enough not to need it', () => {
    const tick = bytesAxisFormatter(50 * 1024 ** 2);
    expect(tick(50 * 1024 ** 2)).toBe('50 MB');
  });

  it('falls back to bytes for an empty axis', () => {
    const tick = bytesAxisFormatter(0);
    expect(tick(0)).toBe('0 B');
    expect(tick(512)).toBe('512 B');
  });
});
