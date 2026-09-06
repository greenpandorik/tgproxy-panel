import { cn } from '@/lib/utils';

import type { NodeEngine } from '@/api/types';

/**
 * Which proxy a node runs, written the way the API spells it.
 *
 * Both engines get the same neutral hairline tag: an engine is a fact about the
 * machine, not a state, and colour in this panel is reserved for things that are
 * wrong. The name is the whole signal, so it stays mono and untranslated - it is
 * the same word that appears in the install script, the job log and the docs.
 */
export function EngineTag({ engine, className }: { engine: NodeEngine; className?: string }) {
  return (
    <span className={cn('mono rounded-sm border border-hairline px-1.5 py-0.5 text-xs text-mute', className)}>{engine}</span>
  );
}
