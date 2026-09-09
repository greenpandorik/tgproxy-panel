import { useQuery } from '@tanstack/react-query';

// Liveness of the panel process itself, for the sidebar footer line.
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
