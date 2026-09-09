import type { CSSProperties } from 'react';

/** Class that carries the fade + 2px rise. Pair it with `enterDelay`. */
export const ENTER_CLASS = 'tgwp-enter';

/** Gap between two consecutive elements, in ms. Matches `.tgwp-enter`. */
export const ENTER_STAGGER_MS = 40;

/** Elements after this many share the last delay, so a long list never crawls in. */
export const ENTER_MAX_STAGGERED = 6;

/** The custom property `.tgwp-enter` reads to work out its delay. */
export function enterDelay(index = 0): CSSProperties {
  const clamped = Math.max(0, Math.min(index, ENTER_MAX_STAGGERED - 1));
  return { '--enter-index': clamped } as CSSProperties;
}

// Class name and delay together, ready to spread onto an element: `<Panel {...enter(i)} />`.
export function enter(index = 0): { className: string; style: CSSProperties } {
  return { className: ENTER_CLASS, style: enterDelay(index) };
}
