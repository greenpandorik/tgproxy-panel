import { describe, expect, it } from 'vitest';

import { pageItems } from './pageItems';

describe('pageItems', () => {
  it('lists every page when there are few', () => {
    expect(pageItems(1, 1)).toEqual([1]);
    expect(pageItems(2, 4)).toEqual([1, 2, 3, 4]);
  });

  it('keeps the first pages together at the start', () => {
    expect(pageItems(1, 6)).toEqual([1, 2, 3, 'gap', 6]);
    expect(pageItems(2, 6)).toEqual([1, 2, 3, 'gap', 6]);
  });

  it('shows a lone skipped page instead of a gap', () => {
    expect(pageItems(3, 6)).toEqual([1, 2, 3, 4, 5, 6]);
  });

  it('puts gaps on both sides in the middle of a long list', () => {
    expect(pageItems(10, 20)).toEqual([1, 'gap', 9, 10, 11, 'gap', 20]);
    expect(pageItems(20, 20)).toEqual([1, 'gap', 18, 19, 20]);
  });
});
