import { describe, expect, it } from 'vitest';

import { formatStars } from './format';

describe('formatStars', () => {
  it.each([
    [0, '0'],
    [42, '42'],
    [999, '999'],
    [1000, '1k'],
    [1500, '1.5k'],
    [6096, '6.1k'],
    [12000, '12k'],
    [12049, '12k'],
    [999950, '1000k'],
  ])('formats %i as %s', (stars, expected) => {
    expect(formatStars(stars)).toBe(expected);
  });

  it('returns an empty string when the count is unknown (-1)', () => {
    expect(formatStars(-1)).toBe('');
  });
});
