import { useQuery } from '@tanstack/react-query';

import { api } from '@/lib/api';

/** `GET /api/v1/status/public` - the only unauthenticated read the panel exposes about itself. */
export interface PublicStatus {
  version: string;
  nodes_total: number;
  nodes_online: number;
  relay_commit: string;
}

/**
 * Public panel status: version, node counts and the relay commit, with no
 * hostnames or names in it. Served without a session (the login screen shows
 * it), cached 10s on the server, so a slow poll here is enough.
 */
export const usePublicStatus = (options: { refetchInterval?: number } = {}) =>
  useQuery({
    queryKey: ['status', 'public'],
    queryFn: () => api.get<PublicStatus>('/api/v1/status/public'),
    staleTime: 60_000,
    refetchInterval: options.refetchInterval ?? 300_000,
    retry: false,
  });
