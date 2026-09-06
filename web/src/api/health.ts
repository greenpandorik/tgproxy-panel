import { useQuery } from '@tanstack/react-query';

/**
 * Liveness of the panel process itself, for the sidebar footer line.
 *
 * `/healthz` is outside /api/v1 and answers plain text, so it does not go
 * through the api wrapper (no CSRF, no 401 broadcast) - a failed fetch here
 * means the browser could not reach the panel, which is exactly the thing the
 * footer reports. Polled once a minute; the "db" half of that line comes from
 * whether the dashboard summary query (which does hit postgres) is erroring.
 */
export const usePanelHealth = () =>
  useQuery({
    queryKey: ['healthz'],
    queryFn: async () => {
      const res = await fetch('/healthz', { headers: { Accept: 'text/plain' } });
      return res.ok;
    },
    refetchInterval: 60_000,
    retry: false,
    staleTime: 30_000,
  });
