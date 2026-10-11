import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
export interface RecoveryPolicy {
  recovery: 'observe' | 'restart';
  maintenance: boolean;
  failure_threshold: number;
  cooldown_seconds: number;
  max_actions_hour: number;
  egress: 'unmanaged' | 'direct' | 'socks5';
  socks_address: string;
  reserve_socks_address: string;
  automatic_failover: boolean;
}
export interface RecoveryState {
  policy: RecoveryPolicy;
  active: string;
  events: { at: string; action: string; result: string }[];
  last: string;
}
export interface ReliabilityReport {
  version: number;
  at: string;
  connections: { seconds: number; success: number; failed: number; at: string } | null;
  routes: { kind: string; healthy: boolean; age_seconds: number; dcs: number }[];
  resources: {
    fd_used: number | null;
    fd_limit: number | null;
    conntrack_used: number | null;
    conntrack_limit: number | null;
    inodes_used_percent: number | null;
    tcp_retransmits_total: number | null;
    listen_drops_total: number | null;
    oom_kills_total: number | null;
  };
  active_egress: string;
  events: RecoveryState['events'];
  policy: RecoveryPolicy;
  me_pool?: MePoolReport | null;
}
export interface MePoolReport {
  at: string;
  enabled: boolean;
  reason?: string;
  state: {
    active_generation: number;
    pending_generation: number;
    pending_age_seconds: number | null;
    hardswap_enabled: boolean;
    pending: boolean;
    pending_map_current: boolean | null;
    pending_writers: number | null;
    writer_deficit: number | null;
    missing_dc_groups: number | null;
    orphan_warm_writers: number | null;
    replacement_preparing: number | null;
    replacement_retiring: number | null;
    orphan_observed_seconds: number | null;
    replacement_observed_seconds: number | null;
  } | null;
}
export const useReliability = (id: string, enabled = true) =>
  useQuery({
    queryKey: ['reliability', id],
    queryFn: () => api.get<RecoveryState>(`/api/v1/nodes/${id}/reliability`),
    enabled: !!id && enabled,
    retry: false,
  });
export function useSaveReliability(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (p: RecoveryPolicy & { restore_primary?: boolean }) =>
      api.put<RecoveryState>(`/api/v1/nodes/${id}/reliability`, p),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['reliability', id] });
      void qc.invalidateQueries({ queryKey: ['nodes'] });
    },
  });
}
