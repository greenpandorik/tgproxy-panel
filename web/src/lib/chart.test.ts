import { describe, expect, it } from 'vitest';

import { MIN_HUE_SEPARATION_DEG, NEUTRAL_SERIES, hexHue, hueGap, seriesColorsCollide, seriesPalette } from './chart';

const distinct = (colors: string[]) => new Set(colors.map((c) => c.toLowerCase())).size === colors.length;

describe('hexHue', () => {
  it('reads the hue of a hex colour', () => {
    expect(hexHue('#ef4444')).toBeCloseTo(0, 0);
    expect(hexHue('#22c55e')).toBeCloseTo(142, 0);
    expect(hexHue('#3b82f6')).toBeCloseTo(217, 0);
  });

  it('expands three-digit hex', () => {
    expect(hexHue('#f00')).toBeCloseTo(0, 0);
  });

  it('has no hue for greys or unparseable input', () => {
    expect(hexHue('#7a7a7a')).toBeNull();
    expect(hexHue('var(--dim)')).toBeNull();
  });
});

describe('hueGap', () => {
  it('takes the shorter way round the wheel', () => {
    // 350deg and 10deg are 20deg apart, not 340.
    expect(hueGap('#ff0033', '#ff3300')).toBeLessThan(30);
  });
});

describe('seriesPalette', () => {
  it('leads with the brand pair when the hues are far enough apart', () => {
    const palette = seriesPalette('#3b82f6', '#e11d48', 4);
    expect(palette[0]).toBe('#3b82f6');
    expect(palette[1]).toBe('#e11d48');
    expect(palette).toHaveLength(4);
    expect(distinct(palette)).toBe(true);
  });

  it('returns distinct colours when the brand hues collide', () => {
    // Both blues, ~6deg apart: unusable as two lines on one chart.
    const primary = '#3b82f6';
    const accent = '#4a90f2';
    expect(hueGap(primary, accent)).toBeLessThan(MIN_HUE_SEPARATION_DEG);

    const palette = seriesPalette(primary, accent, 4);
    expect(distinct(palette)).toBe(true);
    expect(palette).toHaveLength(4);
    expect(palette[0]).toBe(primary);
    expect(palette).not.toContain(accent);
    // Slot 2 falls through to the first neutral palette colour.
    expect(palette[1]).toBe(NEUTRAL_SERIES[0]);
  });

  it('treats an identical pair as a collision', () => {
    const palette = seriesPalette('#3B82F6', '#3b82f6', 3);
    expect(distinct(palette)).toBe(true);
    expect(palette[1]).toBe(NEUTRAL_SERIES[0]);
  });

  it('never hands out the same colour twice when the accent is already in the palette', () => {
    // The DB default accent (#22c55e) is also a neutral palette colour.
    const palette = seriesPalette('#3b82f6', '#22c55e', 5);
    expect(distinct(palette)).toBe(true);
    expect(palette.filter((c) => c === '#22c55e')).toHaveLength(1);
  });

  it('keeps colours stable as the series count grows', () => {
    const small = seriesPalette('#3b82f6', '#e11d48', 2);
    const large = seriesPalette('#3b82f6', '#e11d48', 5);
    expect(large.slice(0, 2)).toEqual(small);
  });

  it('runs out rather than inventing hues', () => {
    const palette = seriesPalette('#3b82f6', '#e11d48', 99);
    expect(palette.length).toBeLessThanOrEqual(2 + NEUTRAL_SERIES.length);
    expect(distinct(palette)).toBe(true);
  });

  it('falls back to the palette when the brand pair is greyed out', () => {
    expect(seriesColorsCollide('#808080', '#8a8a8a')).toBe(true);
    const palette = seriesPalette('#808080', '#8a8a8a', 3);
    expect(distinct(palette)).toBe(true);
    expect(palette[1]).toBe(NEUTRAL_SERIES[0]);
  });
});
