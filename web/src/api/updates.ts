import { keepPreviousData, useQuery } from '@tanstack/react-query';

import { api } from '@/lib/api';

// `GET /api/v1/status/update`.
export interface UpdateStatus {
  enabled: boolean;
  current: string;
  latest: string;
  latest_url: string;
  published_at: string;
  stars: number;
  repo_url: string;
  update_available: boolean;
  checked_at: string;
  stale: boolean;
}

export const useUpdateStatus = () =>
  useQuery({
    queryKey: ['status', 'update'],
    queryFn: () => api.get<UpdateStatus>('/api/v1/status/update'),
    staleTime: 30 * 60_000,
    refetchInterval: 60 * 60_000,
    retry: 1,
    placeholderData: keepPreviousData,
  });
