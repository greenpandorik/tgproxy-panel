import { useQuery } from '@tanstack/react-query';

import { api } from '@/lib/api';

export interface ProbeCheck {
  status: 'ok' | 'failed' | 'not_run';
  method?: 'authenticated_mtproto';
  latency_ms: number;
}

export interface Probe {
  location: string;
  at: string;
  tls: ProbeCheck;
  http: ProbeCheck;
  faketls: ProbeCheck;
  web: ProbeCheck;
}

export const PROBE_CHECKS = ['tls', 'http', 'faketls', 'web'] as const;

/** A report older than this says nothing about the server now. */
export const PROBE_STALE_MS = 180_000;

export const useNodeProbes = (id: string) =>
  useQuery({
    queryKey: ['probes', id],
    queryFn: async () => ({
      ...(await api.get<{ items: Probe[]; expected_locations: string[] | null }>(`/api/v1/nodes/${id}/probes`)),
      observedAt: Date.now(),
    }),
    refetchInterval: 30000,
  });

/** The fresh report from a location, or undefined when it sent none or only an old one. */
export function freshProbe(data: { items: Probe[]; observedAt: number } | undefined, location: string): Probe | undefined {
  const report = data?.items.find((p) => p.location === location);
  if (!report || (data?.observedAt ?? 0) - Date.parse(report.at) > PROBE_STALE_MS) return undefined;
  return report;
}

/** Every check from the expected locations' fresh reports. */
export function freshProbeChecks(
  data: { items: Probe[]; expected_locations: string[] | null; observedAt: number } | undefined,
): ProbeCheck[] {
  return (data?.expected_locations ?? []).flatMap((location) => {
    const report = freshProbe(data, location);
    return report ? PROBE_CHECKS.map((key) => report[key]) : [];
  });
}
