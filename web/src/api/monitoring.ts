import { useQuery } from '@tanstack/react-query';

import { api } from '@/lib/api';

import type { MonitoringOverview, SeriesPoint } from './types';

/** Time-range choices for the monitoring overview switch. */
export type MonitoringRange = '1h' | '6h' | '24h' | '7d';

/** The ranges in the order the switch offers them - shared by every chart that has one. */
export const MONITORING_RANGES: MonitoringRange[] = ['1h', '6h', '24h', '7d'];

const RANGE_SECONDS: Record<MonitoringRange, number> = {
  '1h': 3600,
  '6h': 6 * 3600,
  '24h': 24 * 3600,
  '7d': 7 * 24 * 3600,
};

// A coarser sample step for wider ranges keeps the response light without losing visible shape.
const RANGE_STEP_SECONDS: Record<MonitoringRange, number> = {
  '1h': 10,
  '6h': 60,
  '24h': 180,
  '7d': 1800,
};

export const monitoringKeys = {
  overview: (range: MonitoringRange) => ['monitoring', 'overview', range] as const,
  nodeSeries: (nodeId: string, range: MonitoringRange) => ['monitoring', 'series', nodeId, range] as const,
};

// One node's raw snapshot series over a range.
export const useNodeSeries = (nodeId: string, range: MonitoringRange) =>
  useQuery({
    queryKey: monitoringKeys.nodeSeries(nodeId, range),
    queryFn: () => {
      const to = new Date();
      const from = new Date(to.getTime() - RANGE_SECONDS[range] * 1000);
      const q = new URLSearchParams({ from: from.toISOString(), to: to.toISOString() });
      return api.get<{ points: SeriesPoint[] }>(`/api/v1/monitoring/nodes/${nodeId}/series?${q.toString()}`);
    },
    enabled: !!nodeId,
    refetchInterval: 60_000,
  });

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
