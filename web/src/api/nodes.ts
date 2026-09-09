import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/lib/api';

import type {
  ApplyJob,
  CreateNodeInput,
  CreateNodeResult,
  InstallCommandResult,
  LiveProfile,
  Node,
  NodeCheckReport,
  NodeHealth,
  NodeSite,
  NodeStats,
  Paginated,
  PatchNodeInput,
  Profile,
  RegistrationSecretResult,
} from './types';

export const nodeKeys = {
  all: ['nodes'] as const,
  one: (id: string) => ['nodes', id] as const,
  health: (id: string) => ['nodes', id, 'health'] as const,
  profiles: (id: string, live?: boolean) => ['nodes', id, 'profiles', live ?? false] as const,
  stats: (id: string) => ['nodes', id, 'stats'] as const,
  jobs: (id: string) => ['nodes', id, 'jobs'] as const,
  site: (id: string) => ['nodes', id, 'site'] as const,
};

const REFRESH_MS = 10_000;

export const useNodes = () =>
  useQuery({ queryKey: nodeKeys.all, queryFn: () => api.get<Paginated<Node>>('/api/v1/nodes'), refetchInterval: REFRESH_MS });

export const useNode = (id: string) =>
  useQuery({
    queryKey: nodeKeys.one(id),
    queryFn: () => api.get<Node>(`/api/v1/nodes/${id}`),
    refetchInterval: REFRESH_MS,
    enabled: !!id,
  });

export const useCreateNode = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: CreateNodeInput) => api.post<CreateNodeResult>('/api/v1/nodes', b),
    onSuccess: () => qc.invalidateQueries({ queryKey: nodeKeys.all }),
  });
};

export const usePatchNode = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: PatchNodeInput) => api.patch<Node>(`/api/v1/nodes/${id}`, b),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: nodeKeys.all });
      void qc.invalidateQueries({ queryKey: nodeKeys.one(id) });
    },
  });
};

export const useDeleteNode = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/api/v1/nodes/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: nodeKeys.all }),
  });
};

export const useInstallCommand = (id: string) =>
  useMutation({
    mutationFn: () => api.get<InstallCommandResult>(`/api/v1/nodes/${id}/install-command`),
  });

// The node's own "default" profile secret - what @MTProxybot's own verification connects
// with when registering the node for a sponsor channel. Unlike install-command this is a
// plain read with no side effect, so it is a query, not a mutation; it never changes on its
// own, so there is nothing to refetch it for.
export const useNodeRegistrationSecret = (id: string, enabled = true) =>
  useQuery({
    queryKey: [...nodeKeys.one(id), 'registration-secret'],
    queryFn: () => api.get<RegistrationSecretResult>(`/api/v1/nodes/${id}/registration-secret`),
    enabled: !!id && enabled,
    retry: false,
    staleTime: Infinity,
  });

export const useNodeHealth = (id: string, enabled = true) =>
  useQuery({
    queryKey: nodeKeys.health(id),
    queryFn: () => api.get<NodeHealth>(`/api/v1/nodes/${id}/health`),
    refetchInterval: REFRESH_MS,
    enabled: !!id && enabled,
    retry: false,
  });

export const useNodeProfiles = (id: string, live = false) =>
  useQuery({
    queryKey: nodeKeys.profiles(id, live),
    queryFn: () => api.get<Paginated<Profile | LiveProfile>>(`/api/v1/nodes/${id}/profiles${live ? '?live=1' : ''}`),
    enabled: !!id,
    retry: false,
  });

export const useNodeStats = (id: string, enabled = true) =>
  useQuery({
    queryKey: nodeKeys.stats(id),
    queryFn: () => api.get<NodeStats>(`/api/v1/nodes/${id}/stats`),
    enabled: !!id && enabled,
    retry: false,
  });

export const useNodeJobs = (id: string, limit = 20) =>
  useQuery({
    queryKey: nodeKeys.jobs(id),
    queryFn: () => api.get<{ items: ApplyJob[] }>(`/api/v1/nodes/${id}/jobs?limit=${limit}`),
    refetchInterval: REFRESH_MS,
    enabled: !!id,
  });

export const useRestartNode = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<void>(`/api/v1/nodes/${id}/restart`),
    onSuccess: () => qc.invalidateQueries({ queryKey: nodeKeys.health(id) }),
  });
};

export const useApplyNode = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<{ queued: boolean }>(`/api/v1/nodes/${id}/apply`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: nodeKeys.one(id) });
      void qc.invalidateQueries({ queryKey: nodeKeys.jobs(id) });
    },
  });
};

export const useRunNodeCheck = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<NodeCheckReport>(`/api/v1/nodes/${id}/check`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: nodeKeys.one(id) });
    },
  });
};

export const useNodeSite = (id: string) =>
  useQuery({ queryKey: nodeKeys.site(id), queryFn: () => api.get<NodeSite>(`/api/v1/nodes/${id}/site`), enabled: !!id });

export const useAssignSite = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (templateId: string) => api.post<NodeSite>(`/api/v1/nodes/${id}/site`, { template_id: templateId }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: nodeKeys.site(id) });
      void qc.invalidateQueries({ queryKey: nodeKeys.one(id) });
    },
  });
};

/** Builds the SSE URL for `NodeLogs` - opened directly via `EventSource`, not through the fetch-based `api` client. */
export function nodeLogsUrl(id: string, services: string[], lines: number, follow: boolean): string {
  const q = new URLSearchParams();
  q.set('services', services.join(','));
  q.set('lines', String(lines));
  q.set('follow', follow ? '1' : '0');
  return `/api/v1/nodes/${id}/logs?${q.toString()}`;
}

/**
 * Queues an apply on every node that has unapplied changes, for the command
 * palette's "Apply everywhere". Requests go out together and failures are
 * counted rather than thrown, so one unreachable node does not hide the rest
 * having been queued; the caller reports `queued` of `total`.
 */
export const useApplyDirtyNodes = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (ids: string[]) => {
      const results = await Promise.allSettled(ids.map((id) => api.post<{ queued: boolean }>(`/api/v1/nodes/${id}/apply`)));
      return { queued: results.filter((r) => r.status === 'fulfilled').length, total: ids.length };
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: nodeKeys.all }),
  });
};
