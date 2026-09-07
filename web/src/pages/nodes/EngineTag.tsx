import { Badge } from '@/components/ui/badge';

import type { NodeEngine } from '@/api/types';

/**
 * Which proxy a node runs, written the way the API spells it.
 *
 * Both engines get the same neutral hairline tag: an engine is a fact about the
 * machine, not a state, and colour in this panel is reserved for things that are
 * wrong. The name is the whole signal, so it stays mono and untranslated - it is
 * the same word that appears in the install script, the job log and the docs.
 *
 * It stays a component because the name is a domain fact worth having one
 * place for, but the shape is the panel's one chip shape: it renders a Badge.
 */
export function EngineTag({ engine, className }: { engine: NodeEngine; className?: string }) {
  return <Badge className={className}>{engine}</Badge>;
}
