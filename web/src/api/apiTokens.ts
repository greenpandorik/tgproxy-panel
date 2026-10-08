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
  list: ['api-tokens', 'list'] as const,
  scopes: ['api-tokens', 'scopes'] as const,
};
export const useApiTokens = () =>
  useQuery({
    queryKey: apiTokenKeys.list,
    queryFn: () => api.get<{ items: ApiToken[]; total: number }>('/api/v1/api-tokens'),
  });
export const useApiTokenScopes = () =>
  useQuery({
    queryKey: apiTokenKeys.scopes,
    queryFn: () => api.get<ApiTokenScopes>('/api/v1/api-tokens/scopes'),
  });

/** Deliver the one-time secret directly to volatile UI state; cache metadata only. */
export function useCreateApiToken(onSecret: (secret: string) => void) {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: async (input: CreateApiTokenInput) => {
      const result = await api.post<{ token: string; api_token: ApiToken }>('/api/v1/api-tokens', input, { cache: 'no-store' });
      onSecret(result.token);
      return result.api_token;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: apiTokenKeys.list });
    },
  });
}
export function useRevokeApiToken() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/api/v1/api-tokens/${encodeURIComponent(id)}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: apiTokenKeys.list }),
  });
}
