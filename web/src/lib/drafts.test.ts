import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  DEFAULT_DRAFT_TTL_MS,
  DRAFT_WRITE_DEBOUNCE_MS,
  clearDraft,
  draftStorageKey,
  formatDraftTime,
  readDraft,
  serializeDraft,
  useDraft,
  writeDraft,
} from './drafts';

interface Form {
  label: string;
  count: number;
}

const INITIAL: Form = { label: '', count: 1 };

function stored(key: string): unknown {
  const raw = window.localStorage.getItem(draftStorageKey(key));
  return raw ? JSON.parse(raw) : null;
}

describe('draft storage', () => {
  beforeEach(() => window.localStorage.clear());

  it('writes and reads a draft back with its timestamp', () => {
    writeDraft('t', { label: 'x', count: 2 }, 1_000);
    expect(readDraft<Form>('t', DEFAULT_DRAFT_TTL_MS, 2_000)).toEqual({ value: { label: 'x', count: 2 }, savedAt: 1_000 });
  });

  it('drops an expired draft on read and removes it from storage', () => {
    writeDraft('t', { label: 'x', count: 2 }, 0);
    expect(readDraft<Form>('t', DEFAULT_DRAFT_TTL_MS, DEFAULT_DRAFT_TTL_MS + 1)).toBeNull();
    expect(window.localStorage.getItem(draftStorageKey('t'))).toBeNull();
  });

  it('treats a malformed entry as absent', () => {
    window.localStorage.setItem(draftStorageKey('t'), '{not json');
    expect(readDraft('t')).toBeNull();
    window.localStorage.setItem(draftStorageKey('t'), '{"nope":1}');
    expect(readDraft('t')).toBeNull();
    expect(window.localStorage.getItem(draftStorageKey('t'))).toBeNull();
  });

  it('clears a draft', () => {
    writeDraft('t', INITIAL);
    clearDraft('t');
    expect(readDraft('t')).toBeNull();
  });

  it('serializes objects with a fixed key order', () => {
    expect(serializeDraft({ b: 1, a: { d: [2, { z: 1, y: 2 }], c: 3 } })).toBe(
      serializeDraft({ a: { c: 3, d: [2, { y: 2, z: 1 }] }, b: 1 }),
    );
  });

  it('survives a storage that throws', () => {
    const real = window.localStorage;
    const blocked = new Proxy({} as Storage, {
      get() {
        throw new Error('blocked');
      },
    });
    Object.defineProperty(window, 'localStorage', { value: blocked, configurable: true });
    try {
      expect(() => writeDraft('t', INITIAL)).not.toThrow();
      expect(readDraft('t')).toBeNull();
      expect(() => clearDraft('t')).not.toThrow();
    } finally {
      Object.defineProperty(window, 'localStorage', { value: real, configurable: true });
    }
  });

  it('formats a draft from today as a clock time and an older one with its date', () => {
    const now = new Date(2026, 8, 6, 12, 30).getTime();
    expect(formatDraftTime(now - 60_000, 'ru', now)).toBe('12:29');
    expect(formatDraftTime(now - 24 * 3_600_000, 'ru', now)).toMatch(/05\.09\.2026.*12:30/);
  });
});

describe('useDraft', () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it('writes the current values after the debounce, but never while they equal initial', () => {
    const { rerender } = renderHook(({ current }: { current: Form }) => useDraft('t', current, { initial: INITIAL }), {
      initialProps: { current: INITIAL },
    });
    act(() => void vi.advanceTimersByTime(DRAFT_WRITE_DEBOUNCE_MS * 2));
    expect(stored('t')).toBeNull();

    rerender({ current: { label: 'a', count: 1 } });
    act(() => void vi.advanceTimersByTime(DRAFT_WRITE_DEBOUNCE_MS - 1));
    expect(stored('t')).toBeNull();
    act(() => void vi.advanceTimersByTime(1));
    expect(stored('t')).toMatchObject({ value: { label: 'a', count: 1 } });
  });

  it('flushes a pending write when the dialog closes within the debounce window', () => {
    const { rerender } = renderHook(
      ({ open, current }: { open: boolean; current: Form }) => useDraft('t', current, { initial: INITIAL, open }),
      { initialProps: { open: true, current: INITIAL } },
    );
    rerender({ open: true, current: { label: 'typed', count: 1 } });
    // Closed and reset in the same event, well before the debounce fires.
    rerender({ open: false, current: INITIAL });
    expect(stored('t')).toMatchObject({ value: { label: 'typed', count: 1 } });
  });

  it('drops a pending write when the open form returns to its initial values', () => {
    const { rerender } = renderHook(({ current }: { current: Form }) => useDraft('t', current, { initial: INITIAL }), {
      initialProps: { current: INITIAL },
    });
    rerender({ current: { label: 'typed', count: 1 } });
    rerender({ current: INITIAL });
    act(() => void vi.advanceTimersByTime(DRAFT_WRITE_DEBOUNCE_MS * 2));
    expect(stored('t')).toBeNull();
  });

  it('does not store a form that has not yet been filled in from the server', () => {
    const loaded: Form = { label: 'from server', count: 5 };
    const { rerender } = renderHook(({ current }: { current: Form }) => useDraft('t', current, { initial: loaded }), {
      // Mounted with placeholder defaults; the reset to the loaded values follows.
      initialProps: { current: INITIAL },
    });
    act(() => void vi.advanceTimersByTime(DRAFT_WRITE_DEBOUNCE_MS * 2));
    expect(stored('t')).toBeNull();

    rerender({ current: loaded });
    rerender({ current: { ...loaded, label: 'edited' } });
    act(() => void vi.advanceTimersByTime(DRAFT_WRITE_DEBOUNCE_MS));
    expect(stored('t')).toMatchObject({ value: { label: 'edited', count: 5 } });
  });

  it('flushes a pending write on unmount', () => {
    const { rerender, unmount } = renderHook(({ current }: { current: Form }) => useDraft('t', current, { initial: INITIAL }), {
      initialProps: { current: INITIAL },
    });
    rerender({ current: { label: 'typed', count: 1 } });
    unmount();
    expect(stored('t')).toMatchObject({ value: { label: 'typed', count: 1 } });
  });

  it('offers a stored draft when the form opens and hides it after dismiss', () => {
    writeDraft('t', { label: 'saved', count: 3 });
    const { result } = renderHook(() => useDraft('t', INITIAL, { initial: INITIAL }));
    expect(result.current.hasDraft).toBe(true);
    expect(result.current.draft?.value).toEqual({ label: 'saved', count: 3 });

    act(() => result.current.dismiss());
    expect(result.current.hasDraft).toBe(false);
    // Dismissing keeps the stored draft; only clear() removes it.
    expect(stored('t')).not.toBeNull();
  });

  it('does not offer a draft that equals the initial values', () => {
    writeDraft('t', { count: 1, label: '' });
    const { result } = renderHook(() => useDraft('t', INITIAL, { initial: INITIAL }));
    expect(result.current.hasDraft).toBe(false);
  });

  it('ignores an expired draft', () => {
    writeDraft('t', { label: 'old', count: 9 }, Date.now() - DEFAULT_DRAFT_TTL_MS - 1);
    const { result } = renderHook(() => useDraft('t', INITIAL, { initial: INITIAL }));
    expect(result.current.hasDraft).toBe(false);
    expect(stored('t')).toBeNull();
  });

  it('clear() removes the draft and cancels a write still waiting', () => {
    writeDraft('t', { label: 'saved', count: 3 });
    const { result, rerender } = renderHook(({ current }: { current: Form }) => useDraft('t', current, { initial: INITIAL }), {
      initialProps: { current: INITIAL },
    });
    rerender({ current: { label: 'typed', count: 1 } });
    act(() => result.current.clear());
    act(() => void vi.advanceTimersByTime(DRAFT_WRITE_DEBOUNCE_MS * 2));
    expect(result.current.hasDraft).toBe(false);
    expect(stored('t')).toBeNull();
  });

  it('re-reads the draft each time a closed dialog opens, and writes nothing while closed', () => {
    const { result, rerender } = renderHook(
      ({ open, current }: { open: boolean; current: Form }) => useDraft('t', current, { initial: INITIAL, open }),
      { initialProps: { open: false, current: { label: 'stale', count: 1 } } },
    );
    act(() => void vi.advanceTimersByTime(DRAFT_WRITE_DEBOUNCE_MS * 2));
    expect(stored('t')).toBeNull();
    expect(result.current.hasDraft).toBe(false);

    writeDraft('t', { label: 'from-last-time', count: 2 });
    rerender({ open: true, current: INITIAL });
    expect(result.current.draft?.value).toEqual({ label: 'from-last-time', count: 2 });
  });

  it('switches drafts when the key changes', () => {
    writeDraft('a', { label: 'A', count: 1 });
    writeDraft('b', { label: 'B', count: 1 });
    const { result, rerender } = renderHook(({ key }: { key: string }) => useDraft(key, INITIAL, { initial: INITIAL }), {
      initialProps: { key: 'a' },
    });
    expect(result.current.draft?.value.label).toBe('A');
    rerender({ key: 'b' });
    expect(result.current.draft?.value.label).toBe('B');
  });
});
