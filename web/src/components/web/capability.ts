import type { Node, TelemtCapabilities } from '@/api/types';

/** Capability names, matching internal/telemt.Cap* exactly. */
export const CAP_WEB = 'Web';
export const CAP_CARRIER_NEGOTIATION = 'CarrierNegotiation';
export const CAP_CARRIER_LEARNING = 'CarrierLearning';
export const CAP_CARRIER_LEARNING_RESET = 'CarrierLearningReset';
export const CAP_WEB_RUNTIME = 'WebRuntime';

/**
 * The three answers to "can this node do X".
 * `supported` and `unsupported` are the node's answer; `undetermined` is the panel admitting
 * it never got one, and must never be drawn as either of the other two.
 */
export type CapabilityState = 'supported' | 'unsupported' | 'undetermined';

export function capabilityState(caps: TelemtCapabilities | null | undefined, name: string): CapabilityState {
  if (!caps) return 'undetermined';
  const value = caps[name];
  if (value === null || value === undefined) return 'undetermined';
  return value ? 'supported' : 'unsupported';
}

/** The same question asked of a node, which also has to run telemt at all. */
export function nodeCapability(node: Pick<Node, 'engine' | 'telemt_capabilities'>, name: string): CapabilityState {
  if (node.engine !== 'telemt') return 'unsupported';
  return capabilityState(node.telemt_capabilities, name);
}

/** How a set of nodes answered the "does this telemt serve WEB" question. */
export interface WebCapabilityCounts {
  supported: number;
  unsupported: number;
  undetermined: number;
  total: number;
}

export function webCapabilityCounts(nodes: Pick<Node, 'engine' | 'telemt_capabilities'>[]): WebCapabilityCounts {
  const counts: WebCapabilityCounts = { supported: 0, unsupported: 0, undetermined: 0, total: nodes.length };
  for (const node of nodes) {
    counts[nodeCapability(node, CAP_WEB)] += 1;
  }
  return counts;
}
