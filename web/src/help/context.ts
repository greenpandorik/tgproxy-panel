import { createContext, useContext } from 'react';

import type { HelpTopic } from './content';

export interface HelpContextValue {
  /** Opens the sheet on a topic (replacing whatever it was showing). */
  open: (topic: HelpTopic) => void;
  close: () => void;
  /** Opens the topic mapped to the current route, if any. */
  openForPage: () => void;
}

export const HelpContext = createContext<HelpContextValue | null>(null);

/** The shell-level help API. Null outside the provider (dialog tests render without it). */
export function useHelp(): HelpContextValue | null {
  return useContext(HelpContext);
}
