import { useCallback, useEffect, useMemo, useState } from 'react';
import { useLocation } from 'react-router-dom';

import { HelpContext } from './context';
import { HelpSheet } from './HelpSheet';
import { topicForPath } from './routes';

import type { ReactNode } from 'react';
import type { HelpTopic } from './content';
import type { HelpContextValue } from './context';

/** What the page-level sheet shows, and on which route it was opened. */
interface SheetState {
  topic: HelpTopic;
  open: boolean;
  pathname: string;
}

/** True when a keystroke belongs to a text field and must not be read as a shortcut. */
function isEditable(el: Element | null): boolean {
  if (!el) return false;
  const tag = el.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
  return (el as HTMLElement).isContentEditable;
}

/** The help button inside the topmost open dialog or drawer, if there is one. */
function helpButtonInOpenDialog(): HTMLElement | null {
  const popups = document.querySelectorAll<HTMLElement>('[data-slot="dialog-content"], [data-slot="sheet-content"]');
  for (let i = popups.length - 1; i >= 0; i--) {
    const btn = popups[i].querySelector<HTMLElement>('[data-help-button]');
    if (btn) return btn;
  }
  return null;
}

/**
 * Mounts the page-level help sheet once, exposes `open(topic)` to the palette,
 * and owns the `?` shortcut: with nothing editable focused, `?` opens the help
 * for the current route - or, when a dialog is open, presses that dialog's own
 * help button, so the sheet that opens is the one about the form in front of
 * the operator rather than the page behind it.
 *
 * The sheet remembers the route it was opened on and counts as open only while
 * the operator is still there: navigating away closes it without an effect.
 */
export function HelpProvider({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  const [sheet, setSheet] = useState<SheetState | null>(null);

  const openTopic = useCallback((topic: HelpTopic) => setSheet({ topic, open: true, pathname }), [pathname]);
  const close = useCallback(() => setSheet((s) => (s ? { ...s, open: false } : s)), []);
  const openForPage = useCallback(() => {
    const topic = topicForPath(pathname);
    if (topic) openTopic(topic);
  }, [pathname, openTopic]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== '?' || e.metaKey || e.ctrlKey || e.altKey || e.isComposing) return;
      if (isEditable(document.activeElement)) return;
      const inDialog = helpButtonInOpenDialog();
      if (inDialog) {
        e.preventDefault();
        inDialog.click();
        return;
      }
      const topic = topicForPath(pathname);
      if (!topic) return;
      e.preventDefault();
      // A second `?` closes what the first one opened.
      setSheet((s) =>
        s && s.open && s.pathname === pathname && s.topic === topic ? { ...s, open: false } : { topic, open: true, pathname },
      );
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [pathname]);

  const value = useMemo<HelpContextValue>(() => ({ open: openTopic, close, openForPage }), [openTopic, close, openForPage]);
  const isOpen = !!sheet && sheet.open && sheet.pathname === pathname;

  return (
    <HelpContext.Provider value={value}>
      {children}
      {sheet && <HelpSheet topic={sheet.topic} open={isOpen} onOpenChange={(next) => (next ? undefined : close())} />}
    </HelpContext.Provider>
  );
}
