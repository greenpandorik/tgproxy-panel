import { useQuery } from '@tanstack/react-query';

import { api } from '@/lib/api';

import type { AuditEntry, AuditFilters, Paginated } from './types';

export const auditKeys = {
  list: (filters: AuditFilters) => ['audit', 'list', filters] as const,
};

function filtersToQuery(filters: AuditFilters): string {
  const q = new URLSearchParams();
  if (filters.page) q.set('page', String(filters.page));
  if (filters.per_page) q.set('per_page', String(filters.per_page));
  if (filters.action) q.set('action', filters.action);
  if (filters.user) q.set('user', filters.user);
  if (filters.from) q.set('from', filters.from);
  if (filters.to) q.set('to', filters.to);
  const s = q.toString();
  return s ? `?${s}` : '';
}

export const useAudit = (filters: AuditFilters = {}) =>
  useQuery({
    queryKey: auditKeys.list(filters),
    queryFn: () => api.get<Paginated<AuditEntry>>(`/api/v1/audit${filtersToQuery(filters)}`),
    refetchInterval: 30_000,
  });
