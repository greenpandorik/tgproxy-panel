/*
 * Series colour assignment for the panel's charts.
 *
 * The spec puts the operator's own brand pair at the front of every chart:
 * --brand-primary is series 1, --brand-accent is series 2. That is a branding
 * rule, and branding must not win over legibility - two brand hues sitting a
 * few degrees apart on the wheel are two lines nobody can tell apart. So the
 * pair is checked first, and when the hues collide the accent is dropped and
 * the fixed neutral palette takes over from slot 2 onwards.
 *
 * The palette itself is a fixed order, never cycled and never reassigned by
 * rank: a node keeps its colour when the list is filtered or re-sorted.
 */

/** Fallback hues after the brand pair, in fixed order. */
export const NEUTRAL_SERIES = ['#a78bfa', '#f59e0b', '#22c55e', '#ec4899'] as const;

/** Below this hue gap two lines read as the same colour on a dark ground. */
export const MIN_HUE_SEPARATION_DEG = 25;

/** Offline nodes are drawn in --dim: they are not a category, they are absent. */
export const OFFLINE_SERIES_COLOR = 'var(--dim)';

/** Largest number of distinct colours `seriesPalette` can hand out. */
export const MAX_SERIES = 1 + NEUTRAL_SERIES.length;

function parseHex(color: string): [number, number, number] | null {
  const hex = color.trim().replace(/^#/, '');
  if (!/^[0-9a-fA-F]+$/.test(hex)) return null;
  if (hex.length === 3) {
    return [
      Number.parseInt(hex[0] + hex[0], 16),
      Number.parseInt(hex[1] + hex[1], 16),
      Number.parseInt(hex[2] + hex[2], 16),
    ];
  }
  if (hex.length === 6) {
    return [Number.parseInt(hex.slice(0, 2), 16), Number.parseInt(hex.slice(2, 4), 16), Number.parseInt(hex.slice(4, 6), 16)];
  }
  return null;
}

/**
 * Hue of a hex colour in degrees, or null when it has no usable hue - either
 * it does not parse, or it is so close to grey that its hue is noise.
 */
export function hexHue(color: string): number | null {
  const rgb = parseHex(color);
  if (!rgb) return null;
  const [r, g, b] = rgb.map((v) => v / 255);
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const chroma = max - min;
  if (chroma < 0.04) return null;

  let hue: number;
  if (max === r) hue = ((g - b) / chroma) % 6;
  else if (max === g) hue = (b - r) / chroma + 2;
  else hue = (r - g) / chroma + 4;

  hue *= 60;
  return hue < 0 ? hue + 360 : hue;
}

/** Shortest distance between two hues, 0-180 degrees. Null when either colour has no hue. */
export function hueGap(a: string, b: string): number | null {
  const ha = hexHue(a);
  const hb = hexHue(b);
  if (ha === null || hb === null) return null;
  const raw = Math.abs(ha - hb) % 360;
  return raw > 180 ? 360 - raw : raw;
}

/** True when two colours are too close to work as two series in one chart. */
export function seriesColorsCollide(a: string, b: string): boolean {
  if (!a || !b) return true;
  if (a.trim().toLowerCase() === b.trim().toLowerCase()) return true;
  const gap = hueGap(a, b);
  // Two greys have no hue to compare and are equally indistinguishable.
  if (gap === null) return hexHue(a) === null && hexHue(b) === null;
  return gap < MIN_HUE_SEPARATION_DEG;
}

/**
 * `count` distinct series colours: the brand primary, then the brand accent
 * unless it collides with the primary, then the neutral palette. Duplicates
 * are skipped, so a brand hue that already appears in the palette is only
 * ever handed out once. Returns fewer than `count` colours when the palette
 * runs out - the caller folds the remaining series into one "other" bucket.
 */
export function seriesPalette(brandPrimary: string, brandAccent: string, count: number): string[] {
  const out: string[] = [];
  const add = (color: string | undefined | null) => {
    if (!color) return;
    const key = color.trim().toLowerCase();
    if (!key || out.some((c) => c.trim().toLowerCase() === key)) return;
    out.push(color);
  };

  add(brandPrimary);
  if (!seriesColorsCollide(brandPrimary, brandAccent)) add(brandAccent);
  for (const color of NEUTRAL_SERIES) {
    if (out.length >= count) break;
    add(color);
  }

  return out.slice(0, Math.max(0, count));
}
