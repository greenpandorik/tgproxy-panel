import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/lib/api';

import type { Alert, DashboardSummary, DashboardTrends, SeriesPoint } from './types';
import type { UseQueryResult } from '@tanstack/react-query';

export const dashboardKeys = {
  summary: ['dashboard', 'summary'] as const,
  series: (nodeId: string) => ['dashboard', 'series', nodeId] as const,
  alerts: ['dashboard', 'alerts'] as const,
  trends: ['dashboard', 'trends'] as const,
};

export const useDashboardSummary = () =>
  useQuery({
    queryKey: dashboardKeys.summary,
    queryFn: () => api.get<DashboardSummary>('/api/v1/dashboard/summary'),
    refetchInterval: 10_000,
  });

export const useMonitoringSeries = (nodeId: string, from?: string, to?: string) => {
  const q = new URLSearchParams();
  if (from) q.set('from', from);
  if (to) q.set('to', to);
  const qs = q.toString();
  return useQuery({
    queryKey: dashboardKeys.series(nodeId),
    queryFn: () => api.get<{ points: SeriesPoint[] }>(`/api/v1/monitoring/nodes/${nodeId}/series${qs ? `?${qs}` : ''}`),
    enabled: !!nodeId,
    refetchInterval: 30_000,
  });
};

/** Fetches the last 24h monitoring series for several nodes at once (dashboard sessions chart + traffic-today), one query per node, in the same order as `nodeIds`. */
export const useNodesSeries24h = (nodeIds: string[]): UseQueryResult<{ points: SeriesPoint[] }>[] =>
  useQueries({
    queries: nodeIds.map((id) => ({
      queryKey: dashboardKeys.series(id),
      queryFn: () => api.get<{ points: SeriesPoint[] }>(`/api/v1/monitoring/nodes/${id}/series`),
      refetchInterval: 30_000,
    })),
  });

export const useAlerts = () =>
  useQuery({
    queryKey: dashboardKeys.alerts,
    queryFn: () => api.get<{ items: Alert[] }>('/api/v1/alerts'),
    refetchInterval: 15_000,
  });

/** People online over the last day and the traffic moved in it, for the overview's tiles. */
export const useDashboardTrends = () =>
  useQuery({
    queryKey: dashboardKeys.trends,
    queryFn: () => api.get<DashboardTrends>('/api/v1/dashboard/trends'),
    refetchInterval: 120_000,
  });

/** Marks alerts read. They leave the list at once and come back if the panel refuses. */
export const useReadAlerts = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ids: number[]) => api.post<{ resolved: number }>('/api/v1/alerts/resolve', { ids }),
    onMutate: async (ids) => {
      await qc.cancelQueries({ queryKey: dashboardKeys.alerts });
      const previous = qc.getQueryData<{ items: Alert[] }>(dashboardKeys.alerts);
      if (previous) {
        qc.setQueryData<{ items: Alert[] }>(dashboardKeys.alerts, {
          ...previous,
          items: previous.items.filter((a) => !ids.includes(a.id)),
        });
      }
      return { previous };
    },
    onError: (_error, _ids, context) => {
      if (context?.previous) qc.setQueryData(dashboardKeys.alerts, context.previous);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: dashboardKeys.alerts });
      void qc.invalidateQueries({ queryKey: dashboardKeys.summary });
    },
  });
};
