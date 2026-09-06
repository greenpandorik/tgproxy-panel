import { useCallback, useSyncExternalStore } from 'react';

/**
 * Subscribes to a CSS media query from JS.
 *
 * Used where a breakpoint changes *what is rendered* rather than how it looks:
 * the login screen drops its whole left panel below 900px instead of hiding it
 * with CSS, so the status card and its query never exist on a phone.
 *
 * Falls back to `false` where matchMedia is unavailable (jsdom in some setups),
 * i.e. the narrow layout, which is the one that renders everything essential.
 */
export function useMediaQuery(query: string): boolean {
  const subscribe = useCallback(
    (onChange: () => void) => {
      if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return () => {};
      const mql = window.matchMedia(query);
      // Test mocks routinely stub matchMedia without the listener API.
      mql.addEventListener?.('change', onChange);
      return () => mql.removeEventListener?.('change', onChange);
    },
    [query],
  );

  const getSnapshot = useCallback(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false;
    return window.matchMedia(query).matches;
  }, [query]);

  return useSyncExternalStore(subscribe, getSnapshot, () => false);
}
