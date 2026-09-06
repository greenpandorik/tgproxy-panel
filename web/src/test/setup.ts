import '@testing-library/jest-dom/vitest';

// Node's experimental localStorage shadows jsdom's implementation in this
// runtime and exposes no methods, which breaks anything that reads a stored
// preference (src/i18n reads the language). Install a minimal in-memory shim
// when the ambient one is unusable.
if (typeof window !== 'undefined' && typeof window.localStorage?.getItem !== 'function') {
  const store = new Map<string, string>();
  const shim: Storage = {
    get length() {
      return store.size;
    },
    clear: () => store.clear(),
    getItem: (k: string) => store.get(k) ?? null,
    key: (i: number) => [...store.keys()][i] ?? null,
    removeItem: (k: string) => void store.delete(k),
    setItem: (k: string, v: string) => void store.set(k, String(v)),
  };
  Object.defineProperty(window, 'localStorage', { value: shim, configurable: true });
}

// cmdk (the ⌘K palette) observes its list for size changes and scrolls the
// selected item into view; jsdom has neither API. No-op stubs are enough -
// nothing under test asserts on measured sizes or scroll position.
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
}

if (typeof Element !== 'undefined' && typeof Element.prototype.scrollIntoView !== 'function') {
  Element.prototype.scrollIntoView = () => {};
}
