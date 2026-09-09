import { expect, it } from 'vitest';
import { brandForeground } from './contrast';
it('keeps labels readable for very light, dark and middle brand colors', () => {
  expect(brandForeground('#fff')).toBe('#000000');
  expect(brandForeground('#000')).toBe('#ffffff');
  expect(brandForeground('#206bc4')).toBe('#ffffff');
  expect(brandForeground('#808080')).toBe('#000000');
  expect(brandForeground('#ffff00')).toBe('#000000');
});
