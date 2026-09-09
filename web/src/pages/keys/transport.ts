import type { CarrierMode, Node } from '@/api/types';

export const CARRIER_MODES: CarrierMode[] = ['https', 'https-lanes', 'websocket', 'websocket-lanes'];

export type TransportScope = 'none' | 'telemt' | 'tproxy' | 'mixed';

export function carrierSlug(mode: CarrierMode): string {
  return mode.replace(/-/g, '_');
}

/** Which engines the chosen nodes run - a carrier mode is only a real setting on tproxy. */
export function transportScope(nodes: Node[], selectedIds: string[]): TransportScope {
  const chosen = nodes.filter((n) => selectedIds.includes(n.id));
  if (chosen.length === 0) return 'none';
  const telemt = chosen.some((n) => n.engine === 'telemt');
  const tproxy = chosen.some((n) => n.engine === 'tproxy');
  if (telemt && tproxy) return 'mixed';
  return telemt ? 'telemt' : 'tproxy';
}
