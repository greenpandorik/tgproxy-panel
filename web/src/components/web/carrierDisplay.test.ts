import { describe, expect, it } from 'vitest';

import { carrierRows, distributionIsAbsent, drawableRows, formatShare } from './carrierDisplay';

import type { WebCarrierShare } from '@/api/types';

const api = (carrier: string, selections: number | null, share: number | null): WebCarrierShare => ({
  carrier,
  selections,
  share,
});

describe('carrierRows', () => {
  it('keeps a carrier no node reported absent instead of folding it to zero', () => {
    const rows = carrierRows([
      api('https', 30, 1),
      api('websocket', null, null),
      api('https-lanes', null, null),
      api('websocket-lanes', null, null),
    ]);

    expect(rows.map((r) => r.carrier)).toEqual(['websocket-lanes', 'websocket', 'https-lanes', 'https']);
    expect(rows.filter((r) => r.share === null)).toHaveLength(3);
    expect(rows.some((r) => r.share === 0)).toBe(false);
    expect(rows.find((r) => r.carrier === 'https')).toMatchObject({ selections: 30, share: 1 });
  });

  it('keeps a measured zero, which is a carrier that was chosen no times', () => {
    const rows = carrierRows([api('https', 0, 0), api('websocket', 8, 1)]);

    expect(rows.find((r) => r.carrier === 'https')).toMatchObject({ selections: 0, share: 0 });
  });

  it('treats a carrier the API omitted entirely as absent', () => {
    const rows = carrierRows([api('https', 5, 1)]);

    expect(rows.filter((r) => r.share === null).map((r) => r.carrier)).toEqual([
      'websocket-lanes',
      'websocket',
      'https-lanes',
    ]);
  });

  it('reports the whole distribution as absent only when nothing was measured', () => {
    expect(distributionIsAbsent(carrierRows([]))).toBe(true);
    expect(distributionIsAbsent(carrierRows([api('https', 0, 0)]))).toBe(false);
  });
});

describe('drawableRows', () => {
  it('draws only the measured shares, largest first', () => {
    const rows = drawableRows(
      carrierRows([api('https', 25, 0.25), api('websocket-lanes', 75, 0.75), api('websocket', 0, 0), api('https-lanes', null, null)]),
    );

    expect(rows.map((r) => r.carrier)).toEqual(['websocket-lanes', 'https']);
    expect(rows.map((r) => r.share)).toEqual([0.75, 0.25]);
  });
});

describe('formatShare', () => {
  it('renders a share as a percentage', () => {
    expect(formatShare(0.25, 'en')).toBe('25%');
    expect(formatShare(0, 'en')).toBe('0%');
  });
});
