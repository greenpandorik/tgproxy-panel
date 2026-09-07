import type { CSSProperties } from 'react';

/*
 * The panel's one entrance animation.
 *
 * Everything that arrives - a row of stat tiles, the panels of a page, the
 * first page of a table - fades in and rises 2px, staggered 40ms apart. The
 * point is to say "this just loaded", so the stagger is capped: after the
 * sixth element the delay stops growing and the rest land together. Anything
 * longer stops being feedback and becomes a performance.
 *
 * The animation itself is `.tgwp-enter` in src/index.css, which also turns
 * itself off under prefers-reduced-motion. This module only supplies the
 * class name and the per-element index, so pages never hand-roll a delay.
 *
 * Usage:
 *   {items.map((item, i) => <Panel key={item.id} {...enter(i)} />)}
 *
 * or, when the element already has a className of its own:
 *   <div className={cn(ENTER_CLASS, 'grid gap-4')} style={enterDelay(2)} />
 */

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

/**
 * Class name and delay together, ready to spread onto an element:
 * `<Panel {...enter(i)} />`.
 */
export function enter(index = 0): { className: string; style: CSSProperties } {
  return { className: ENTER_CLASS, style: enterDelay(index) };
}
