import { Badge } from '@/components/ui/badge';

import type { NodeEngine } from '@/api/types';

// Which proxy a node runs, written the way the API spells it.
export function EngineTag({ engine, className }: { engine: NodeEngine; className?: string }) {
  return <Badge className={className}>{engine}</Badge>;
}
