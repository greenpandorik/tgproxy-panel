import { useQuery } from '@tanstack/react-query';

import { api } from '@/lib/api';

import type { MonitoringOverview } from './types';

/** Time-range choices for the monitoring overview switch. */
export type MonitoringRange = '1h' | '6h' | '24h' | '7d';

const RANGE_SECONDS: Record<MonitoringRange, number> = {
  '1h': 3600,
  '6h': 6 * 3600,
  '24h': 24 * 3600,
  '7d': 7 * 24 * 3600,
};

// A coarser sample step for wider ranges keeps the response light without
// losing visible shape - the backend still computes rates from every raw
// snapshot pair before thinning to this spacing.
const RANGE_STEP_SECONDS: Record<MonitoringRange, number> = {
  '1h': 10,
  '6h': 60,
  '24h': 180,
  '7d': 1800,
};

export const monitoringKeys = {
  overview: (range: MonitoringRange) => ['monitoring', 'overview', range] as const,
};

export const useMonitoringOverview = (range: MonitoringRange) =>
  useQuery({
    queryKey: monitoringKeys.overview(range),
    queryFn: () => {
      const to = new Date();
      const from = new Date(to.getTime() - RANGE_SECONDS[range] * 1000);
      const q = new URLSearchParams({ from: from.toISOString(), to: to.toISOString(), step: String(RANGE_STEP_SECONDS[range]) });
      return api.get<MonitoringOverview>(`/api/v1/monitoring/overview?${q.toString()}`);
    },
    refetchInterval: 60_000,
  });
