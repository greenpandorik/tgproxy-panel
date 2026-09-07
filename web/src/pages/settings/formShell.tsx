import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';

import type { ReactNode } from 'react';

/*
 * The shape the seven forms behind the settings tabs share.
 *
 * Each of them is a head (a PanelHeader saying what the section is), a body
 * (a PanelBody grouping the fields that belong together) and a footer strip
 * carrying the one action that commits the form. Before the phase 8 refit each
 * form put its button somewhere different - inside a panel body, floating
 * under the last panel, or in a panel header next to the help mark - so the
 * seven tabs read as seven products. The pieces below are what keeps them
 * one, and they are deliberately dumb: no state, no queries, just the strip
 * and the entrance wrapper.
 */

/**
 * Panels arrive with the entrance, staggered. `Panel` accepts a className but
 * not a style, and the stagger is a custom property, so the delay rides this
 * wrapper rather than the section itself.
 */
export function Arriving({ index = 0, children }: { index?: number; children: ReactNode }) {
  return (
    <div className={ENTER_CLASS} style={enterDelay(index)}>
      {children}
    </div>
  );
}

/**
 * The strip a settings form ends with: whatever needs saying on the left (why
 * the action is missing, what it will do), the action itself on the right.
 */
export function FormFooter({ note, children }: { note?: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-surface border border-hairline bg-card px-4 py-3">
      {note && <p className="min-w-0 text-label text-mute">{note}</p>}
      {children && <div className="ml-auto flex flex-wrap items-center gap-2">{children}</div>}
    </div>
  );
}

/*
 * The third state a form shows instead of itself - the query behind it failed
 * - is not here: it is components/common/ErrorState.tsx, because the settings
 * forms were never the only place that needed it.
 */
