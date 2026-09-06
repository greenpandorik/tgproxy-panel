import { QueryClient } from '@tanstack/react-query';

import { ApiError } from './api';

// A 401/403 means "not allowed", not "transient failure" - retrying just
// spams the backend and delays the unauthorized redirect. Everything else
// gets a couple of retries, matching react-query's default intent.
function shouldRetry(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError && (error.status === 401 || error.status === 403)) return false;
  return failureCount < 2;
}

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: shouldRetry,
      refetchOnWindowFocus: false,
      staleTime: 5_000,
    },
    mutations: {
      retry: false,
    },
  },
});
