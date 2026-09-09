
import { useEffect, useRef, useState } from 'react';

export const DRAFT_STORAGE_PREFIX = 'tgwp.draft.';
export const DEFAULT_DRAFT_TTL_MS = 24 * 60 * 60 * 1000;
export const DRAFT_WRITE_DEBOUNCE_MS = 300;

export interface StoredDraft<T> {
  value: T;
  /** Epoch milliseconds of the last write. */
  savedAt: number;
}

export function draftStorageKey(key: string): string {
  return `${DRAFT_STORAGE_PREFIX}${key}`;
}

export function serializeDraft(value: unknown): string {
  return JSON.stringify(value, (_key, v: unknown) => {
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      const sorted: Record<string, unknown> = {};
      for (const k of Object.keys(v as Record<string, unknown>).sort()) {
        sorted[k] = (v as Record<string, unknown>)[k];
      }
      return sorted;
    }
    return v;
  });
}

/** Reads a draft; an expired or malformed entry is removed and reported as absent. */
export function readDraft<T>(key: string, ttl = DEFAULT_DRAFT_TTL_MS, now = Date.now()): StoredDraft<T> | null {
  try {
    const raw = window.localStorage.getItem(draftStorageKey(key));
    if (!raw) return null;
    const parsed: unknown = JSON.parse(raw);
    if (
      !parsed ||
      typeof parsed !== 'object' ||
      typeof (parsed as StoredDraft<T>).savedAt !== 'number' ||
      !('value' in parsed) ||
      now - (parsed as StoredDraft<T>).savedAt > ttl
    ) {
      window.localStorage.removeItem(draftStorageKey(key));
      return null;
    }
    return parsed as StoredDraft<T>;
  } catch {
    return null;
  }
}

export function writeDraft<T>(key: string, value: T, now = Date.now()): void {
  writeSerializedDraft(key, serializeDraft(value), now);
}

function writeSerializedDraft(key: string, serialized: string, now = Date.now()): void {
  try {
    window.localStorage.setItem(draftStorageKey(key), `{"savedAt":${now},"value":${serialized}}`);
  } catch {
    // Quota exceeded or storage disabled: the form still works, it just won't remember.
  }
}

export function clearDraft(key: string): void {
  try {
    window.localStorage.removeItem(draftStorageKey(key));
  } catch {
    // ignore
  }
}

export function formatDraftTime(savedAt: number, locale?: string, now = Date.now()): string {
  const d = new Date(savedAt);
  const today = new Date(now);
  const sameDay = d.getFullYear() === today.getFullYear() && d.getMonth() === today.getMonth() && d.getDate() === today.getDate();
  return new Intl.DateTimeFormat(locale, sameDay ? { timeStyle: 'short' } : { dateStyle: 'short', timeStyle: 'short' }).format(d);
}

export interface UseDraftOptions<T> {
  /** The form's pristine values; nothing is stored while `current` equals them. */
  initial: T;
  ttl?: number;
  open?: boolean;
}

export interface UseDraftResult<T> {
  draft: StoredDraft<T> | null;
  hasDraft: boolean;
  /** "Continue": hides the banner; the caller loads `draft.value` into the form. */
  dismiss(): void;
  /** "Start over" and a successful submit: removes the stored draft and hides the banner. */
  clear(): void;
}

interface Unsaved {
  key: string;
  serialized: string;
}

export function useDraft<T>(
  key: string,
  current: T,
  { initial, ttl = DEFAULT_DRAFT_TTL_MS, open = true }: UseDraftOptions<T>,
): UseDraftResult<T> {
  const [pending, setPending] = useState<StoredDraft<T> | null>(() => (open ? readDraft<T>(key, ttl) : null));

  // Re-read the stored draft whenever the form (re)opens or switches to another key.
  const [seen, setSeen] = useState({ open, key });
  if (seen.open !== open || seen.key !== key) {
    setSeen({ open, key });
    setPending(open ? readDraft<T>(key, ttl) : null);
  }

  const serializedCurrent = serializeDraft(current);
  const serializedInitial = serializeDraft(initial);

  const timerRef = useRef<number | null>(null);
  // Set only while a debounced write is waiting; null again once written or dropped.
  const unsavedRef = useRef<Unsaved | null>(null);
  // The key whose form has been seen at its initial values while open.
  const armedForRef = useRef<string | null>(null);

  useEffect(() => {
    const cancel = () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      unsavedRef.current = null;
    };
    const flush = () => {
      const unsaved = unsavedRef.current;
      cancel();
      if (unsaved) writeSerializedDraft(unsaved.key, unsaved.serialized);
    };

    if (!open) {
      flush();
      armedForRef.current = null;
      return;
    }
    if (serializedCurrent === serializedInitial) {
      cancel();
      armedForRef.current = key;
      return;
    }
    if (armedForRef.current !== key) return;

    unsavedRef.current = { key, serialized: serializedCurrent };
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      const unsaved = unsavedRef.current;
      unsavedRef.current = null;
      if (unsaved) writeSerializedDraft(unsaved.key, unsaved.serialized);
    }, DRAFT_WRITE_DEBOUNCE_MS);

    return () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [key, open, serializedCurrent, serializedInitial]);

  // Unmount: whatever is still waiting for its debounce goes to storage.
  useEffect(
    () => () => {
      const unsaved = unsavedRef.current;
      unsavedRef.current = null;
      if (unsaved) writeSerializedDraft(unsaved.key, unsaved.serialized);
    },
    [],
  );

  const draft = pending && serializeDraft(pending.value) !== serializedInitial ? pending : null;

  return {
    draft,
    hasDraft: draft !== null,
    dismiss: () => setPending(null),
    clear: () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      unsavedRef.current = null;
      clearDraft(key);
      setPending(null);
    },
  };
}
