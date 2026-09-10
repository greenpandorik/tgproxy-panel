import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/lib/api';

import type { DiagnosticsRun, WebCarrierStats } from './types';

/** How far back the carrier windows look. telemt counters are cumulative, so this is a delta. */
export const CARRIER_WINDOW_SECONDS = 24 * 3600;

export const webKeys = {
  nodeCarriers: (nodeId: string) => ['web', 'carriers', 'node', nodeId] as const,
  fleetCarriers: () => ['web', 'carriers', 'fleet'] as const,
  diagnostics: (nodeId: string) => ['web', 'diagnostics', nodeId] as const,
};

const REFRESH_MS = 60_000;

function windowQuery(seconds: number): string {
  const to = new Date();
  const from = new Date(to.getTime() - seconds * 1000);
  return new URLSearchParams({ from: from.toISOString(), to: to.toISOString() }).toString();
}

export const useNodeWebCarriers = (nodeId: string, enabled = true) =>
  useQuery({
    queryKey: webKeys.nodeCarriers(nodeId),
    queryFn: () => api.get<WebCarrierStats>(`/api/v1/nodes/${nodeId}/web/carriers?${windowQuery(CARRIER_WINDOW_SECONDS)}`),
    enabled: !!nodeId && enabled,
    refetchInterval: REFRESH_MS,
    retry: false,
  });

export const useFleetWebCarriers = () =>
  useQuery({
    queryKey: webKeys.fleetCarriers(),
    queryFn: () => api.get<WebCarrierStats>(`/api/v1/monitoring/web/carriers?${windowQuery(CARRIER_WINDOW_SECONDS)}`),
    refetchInterval: REFRESH_MS,
    retry: false,
  });

/** The last stored passes, newest first, so the card opens on the most recent result. */
export const useNodeDiagnostics = (nodeId: string, limit = 1) =>
  useQuery({
    queryKey: [...webKeys.diagnostics(nodeId), limit],
    queryFn: () => api.get<{ items: DiagnosticsRun[] }>(`/api/v1/nodes/${nodeId}/diagnostics?limit=${limit}`),
    enabled: !!nodeId,
    retry: false,
  });

export const useRunWebDiagnostics = (nodeId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<DiagnosticsRun>(`/api/v1/nodes/${nodeId}/diagnostics/web`),
    onSuccess: () => qc.invalidateQueries({ queryKey: webKeys.diagnostics(nodeId) }),
  });
};
