import { describe, expect, it } from 'vitest';

import { formatBytes, formatStars, splitBytes } from './format';

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

describe('formatBytes', () => {
  it('prints English units by default', () => {
    expect(formatBytes(0, 1, 'en')).toBe('0 B');
    expect(formatBytes(512, 1, 'en')).toBe('512 B');
    expect(formatBytes(5 * 1024 ** 2, 1, 'en')).toBe('5.0 MB');
  });

  it('prints Russian units in the Russian UI', () => {
    expect(formatBytes(0, 1, 'ru')).toBe('0 Б');
    expect(formatBytes(1536, 1, 'ru')).toBe('1.5 КБ');
    expect(formatBytes(11.6 * 1024 ** 3, 1, 'ru')).toBe('11.6 ГБ');
    expect(splitBytes(5 * 1024 ** 2, 1, 'ru')).toEqual({ value: '5.0', unit: 'МБ' });
  });
});
