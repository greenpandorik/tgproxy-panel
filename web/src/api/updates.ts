import { keepPreviousData, useQuery } from '@tanstack/react-query';

import { api } from '@/lib/api';

/**
 * `GET /api/v1/status/update` - the panel's own release check against GitHub.
 * `stars` is -1 when the count is unknown; `stale` means the server is serving
 * its last good answer because the latest fetch failed.
 */
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

/**
 * The server caches the GitHub answer itself, so the browser polls slowly and
 * keeps whatever it last saw across refetches - a chip that blinks empty once
 * an hour would be worse than one that is briefly out of date.
 */
export const useUpdateStatus = () =>
  useQuery({
    queryKey: ['status', 'update'],
    queryFn: () => api.get<UpdateStatus>('/api/v1/status/update'),
    staleTime: 30 * 60_000,
    refetchInterval: 60 * 60_000,
    retry: 1,
    placeholderData: keepPreviousData,
  });
