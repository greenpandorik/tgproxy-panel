import { useQuery } from '@tanstack/react-query';

import { api } from '@/lib/api';

export interface WebPolicy {
  preset: string;
  carrier: string;
  carriers: string[] | false;
  carrier_learning: boolean;
  carrier_negotiation_aggressiveness: string;
  overload: { preset: 'balanced' | 'high_load' | 'custom'; connection_capacity_action: 'drop' | 'wait' | 'respond' };
  timeouts: {
    carrier_negotiation_deadlines_secs: number[];
    carrier_health_secs: number;
    carrier_learning_secs: number;
    bridge_request_secs: number;
    bridge_retry_secs: number;
    carrier_probe_coalesce_ms: number;
  };
}

export interface PolicyResponse {
  policy: WebPolicy;
  default: WebPolicy;
  overridden: boolean;
}

export const webPolicyKey = (nodeId: string) => ['web-policy', nodeId] as const;

export const useWebPolicy = (nodeId: string) =>
  useQuery({
    queryKey: webPolicyKey(nodeId),
    queryFn: () => api.get<PolicyResponse>(`/api/v1/nodes/${nodeId}/web-policy`),
  });
