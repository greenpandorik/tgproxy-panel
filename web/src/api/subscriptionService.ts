import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/lib/api';
import { sessionGuard } from '@/lib/session';

import { keyKeys } from './keys';

export interface SubscriptionService {
  public_url: string;
  hide_on_panel: boolean;
  token_set: boolean;
  token_created_at: string | null;
  panel_url: string;
  version: string;
  status: { last_seen_at: string | null; version: string; address: string };
  online: boolean;
}

export interface ServiceToken {
  token: string;
  command: string;
}

const serviceKey = ['subscription-service'] as const;

export const useSubscriptionService = () =>
  useQuery({
    queryKey: serviceKey,
    queryFn: () => api.get<SubscriptionService>('/api/v1/subscription-service'),
    refetchInterval: 30_000,
  });

/** The scheme and host subscription links are built on. */
export function subscriptionBase(svc: SubscriptionService | undefined): string {
  return svc?.public_url || svc?.panel_url || window.location.origin;
}

export const usePutSubscriptionService = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: { public_url: string; hide_on_panel: boolean }) =>
      api.put<SubscriptionService>('/api/v1/subscription-service', b),
    onMutate: () => sessionGuard(qc),
    onSuccess: (data, _input, isCurrent) => {
      if (!isCurrent()) return;
      qc.setQueryData(serviceKey, data);
      void qc.invalidateQueries({ queryKey: keyKeys.all });
    },
  });
};

export const useIssueServiceToken = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<ServiceToken>('/api/v1/subscription-service/token'),
    onSuccess: () => void qc.invalidateQueries({ queryKey: serviceKey }),
  });
};

export const useRevokeServiceToken = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.del<void>('/api/v1/subscription-service/token'),
    onSuccess: () => void qc.invalidateQueries({ queryKey: serviceKey }),
  });
};
