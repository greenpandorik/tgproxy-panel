import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/lib/api';

export interface ApiToken {
  id: string;
  name: string;
  prefix: string;
  scopes: string[];
  created_at: string;
  expires_at: string;
  last_used_at: string | null;
  revoked_at: string | null;
}
export interface ApiTokenScope {
  id: string;
  resource: string;
  action: 'read' | 'write';
}
export interface ApiTokenScopes {
  scopes: ApiTokenScope[];
  max_expires_in_days: number;
  max_tokens: number;
}
export interface CreateApiTokenInput {
  name: string;
  expires_in_days: number;
  scopes: string[];
}
export const apiTokenKeys = {
  list: (administratorId: string | undefined) => ['api-tokens', 'list', administratorId] as const,
  scopes: (administratorId: string | undefined) => ['api-tokens', 'scopes', administratorId] as const,
};
export const useApiTokens = (administratorId: string | undefined) =>
  useQuery({
    queryKey: apiTokenKeys.list(administratorId),
    enabled: !!administratorId,
    queryFn: () => api.get<{ items: ApiToken[]; total: number }>('/api/v1/api-tokens'),
  });
export const useApiTokenScopes = (administratorId: string | undefined) =>
  useQuery({
    queryKey: apiTokenKeys.scopes(administratorId),
    enabled: !!administratorId,
    queryFn: () => api.get<ApiTokenScopes>('/api/v1/api-tokens/scopes'),
  });

/** Deliver the one-time secret directly to volatile UI state; cache metadata only. */
export function useCreateApiToken(administratorId: string, onSecret: (secret: string) => void) {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: async (input: CreateApiTokenInput) => {
      const ownerId = administratorId;
      const result = await api.post<{ token: string; api_token: ApiToken }>('/api/v1/api-tokens', input, { cache: 'no-store' });
      onSecret(result.token);
      return { api_token: result.api_token, ownerId };
    },
    onSuccess: (result) => {
      void qc.invalidateQueries({ queryKey: apiTokenKeys.list(result.ownerId), exact: true });
    },
  });
}
export function useRevokeApiToken(administratorId: string | undefined) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      if (!administratorId) throw new Error('Administrator identity is required');
      const ownerId = administratorId;
      await api.del<void>(`/api/v1/api-tokens/${encodeURIComponent(id)}`);
      return ownerId;
    },
    onSuccess: (ownerId) => qc.invalidateQueries({ queryKey: apiTokenKeys.list(ownerId), exact: true }),
  });
}
