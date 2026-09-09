import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/lib/api';

import type {
  AccessKey,
  BulkKeysInput,
  BulkKeysResult,
  KeyFilters,
  KeyInput,
  KeyLinksResult,
  KeyStats,
  LinkKind,
  Paginated,
  PatchKeyInput,
  SubscriptionCreated,
} from './types';

export const keyKeys = {
  all: ['keys'] as const,
  list: (filters: KeyFilters) => ['keys', 'list', filters] as const,
  one: (id: string) => ['keys', id] as const,
  links: (id: string) => ['keys', id, 'links'] as const,
  stats: (id: string, range: KeyStatsRange) => ['keys', id, 'stats', range] as const,
};

/** The two windows the key drawer charts. Both fit inside the 30-day snapshot retention. */
export type KeyStatsRange = '24h' | '7d';

const STATS_RANGE_SECONDS: Record<KeyStatsRange, number> = {
  '24h': 24 * 3600,
  '7d': 7 * 24 * 3600,
};

function filtersToQuery(filters: KeyFilters): string {
  const q = new URLSearchParams();
  if (filters.page) q.set('page', String(filters.page));
  if (filters.per_page) q.set('per_page', String(filters.per_page));
  if (filters.type) q.set('type', filters.type);
  if (filters.status) q.set('status', filters.status);
  if (filters.node) q.set('node', filters.node);
  if (filters.q) q.set('q', filters.q);
  const s = q.toString();
  return s ? `?${s}` : '';
}

export const useKeys = (filters: KeyFilters = {}) =>
  useQuery({
    queryKey: keyKeys.list(filters),
    queryFn: () => api.get<Paginated<AccessKey>>(`/api/v1/keys${filtersToQuery(filters)}`),
    refetchInterval: 15_000,
  });

export const useKey = (id: string) =>
  useQuery({ queryKey: keyKeys.one(id), queryFn: () => api.get<AccessKey>(`/api/v1/keys/${id}`), enabled: !!id });

// Every link of a key, grouped by node.
export const useKeyLinks = (id: string, enabled = true) =>
  useQuery({
    queryKey: keyKeys.links(id),
    queryFn: () => api.get<KeyLinksResult>(`/api/v1/keys/${id}/links`),
    enabled: !!id && enabled,
    retry: false,
  });

/** Per-node traffic and connection samples for one key over the chosen window. */
export const useKeyStats = (id: string, range: KeyStatsRange, enabled = true) =>
  useQuery({
    queryKey: keyKeys.stats(id, range),
    queryFn: () => {
      const to = new Date();
      const from = new Date(to.getTime() - STATS_RANGE_SECONDS[range] * 1000);
      const q = new URLSearchParams({ from: from.toISOString(), to: to.toISOString() });
      return api.get<KeyStats>(`/api/v1/keys/${id}/stats?${q.toString()}`);
    },
    enabled: !!id && enabled,
    refetchInterval: 60_000,
  });

export const useCreateKey = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: KeyInput) => api.post<AccessKey>('/api/v1/keys', b),
    onSuccess: () => qc.invalidateQueries({ queryKey: keyKeys.all }),
  });
};

export const useBatchKeys = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: KeyInput) => api.post<Paginated<AccessKey>>('/api/v1/keys/batch', b),
    onSuccess: () => qc.invalidateQueries({ queryKey: keyKeys.all }),
  });
};

export const useBulkKeys = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: BulkKeysInput) => api.post<BulkKeysResult>('/api/v1/keys/bulk', b),
    onSuccess: () => qc.invalidateQueries({ queryKey: keyKeys.all }),
  });
};

export const usePatchKey = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: PatchKeyInput) => api.patch<AccessKey>(`/api/v1/keys/${id}`, b),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keyKeys.all });
      void qc.invalidateQueries({ queryKey: keyKeys.one(id) });
    },
  });
};

export const useDeleteKey = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/api/v1/keys/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: keyKeys.all }),
  });
};

export const useRevokeKey = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.post<AccessKey>(`/api/v1/keys/${id}/revoke`),
    onSuccess: (_data, id) => {
      void qc.invalidateQueries({ queryKey: keyKeys.all });
      void qc.invalidateQueries({ queryKey: keyKeys.one(id) });
    },
  });
};

export const useRotateKey = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.post<AccessKey>(`/api/v1/keys/${id}/rotate`),
    onSuccess: (_data, id) => {
      void qc.invalidateQueries({ queryKey: keyKeys.all });
      void qc.invalidateQueries({ queryKey: keyKeys.one(id) });
    },
  });
};

export const useBindKey = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (nodeId: string) => api.post<AccessKey>(`/api/v1/keys/${id}/bindings`, { node_id: nodeId }),
    onSuccess: () => qc.invalidateQueries({ queryKey: keyKeys.one(id) }),
  });
};

export const useUnbindKey = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (nodeId: string) => api.del<void>(`/api/v1/keys/${id}/bindings/${nodeId}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: keyKeys.one(id) }),
  });
};

export function keyQrUrl(id: string, nodeId: string, size = 256, kind: LinkKind = 'web'): string {
  return `/api/v1/keys/${id}/qr?node=${nodeId}&kind=${kind}&size=${size}`;
}

/** Display name of a link kind. Both are product names, not translated copy. */
export function linkKindLabel(kind: LinkKind): string {
  return kind === 'tls' ? 'Fake-TLS' : 'WEB';
}

// Creates or rotates the key's subscription link.
export const useCreateSubscription = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<SubscriptionCreated>(`/api/v1/keys/${id}/subscription`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keyKeys.all });
      void qc.invalidateQueries({ queryKey: keyKeys.one(id) });
    },
  });
};

export const useRevokeSubscription = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.del<void>(`/api/v1/keys/${id}/subscription`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keyKeys.all });
      void qc.invalidateQueries({ queryKey: keyKeys.one(id) });
    },
  });
};
