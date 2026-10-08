import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { QueryClient } from '@tanstack/react-query';

import { api, invalidateSessionRequests } from '@/lib/api';
import { advanceSession, sessionGuard } from '@/lib/session';
import { brandingKeys } from './branding';

import type { Admin, LoginResult, Me, SecondFactor, TotpEnrolment, TotpSetup } from './types';
import { isTotpChallenge } from './types';

export const authKeys = {
  me: ['auth', 'me'] as const,
  admins: ['auth', 'admins'] as const,
};

/** Clear private data while keeping the live authentication and public branding observers. */
export function forgetSession(qc: QueryClient, refreshBranding = !!qc.getQueryData(authKeys.me)) {
  advanceSession(qc);
  invalidateSessionRequests();
  // Cancellation is synchronous; it prevents late results, including /me, from restoring data.
  void qc.cancelQueries({
    predicate: (q) => JSON.stringify(q.queryKey) !== JSON.stringify(brandingKeys.active),
  });
  qc.removeQueries({
    predicate: (q) =>
      JSON.stringify(q.queryKey) !== JSON.stringify(authKeys.me) &&
      JSON.stringify(q.queryKey) !== JSON.stringify(brandingKeys.active),
  });
  qc.getMutationCache().clear();
  qc.setQueryData(authKeys.me, null);
  // ThemeProvider survives navigation and must remain attached to this public query.
  if (refreshBranding) void qc.invalidateQueries({ queryKey: brandingKeys.active, exact: true });
}

export const useMe = () =>
  useQuery({
    queryKey: authKeys.me,
    queryFn: ({ signal }) => api.get<Me>('/api/v1/auth/me', { signal }),
    retry: false,
  });

export const useLogin = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { username: string; password: string }) => api.post<LoginResult>('/api/v1/auth/login', input),
    onMutate: () => {
      void qc.cancelQueries({ queryKey: authKeys.me, exact: true });
      return sessionGuard(qc);
    },
    onSuccess: (res, _input, isCurrent) => {
      if (!isCurrent()) return;
      forgetSession(qc, true);
      if (!isTotpChallenge(res)) qc.setQueryData(authKeys.me, res);
    },
  });
};

/** Second step of login: exchanges the challenge for a session. */
export const useTotpVerify = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: SecondFactor & { challenge: string }) => api.post<Me>('/api/v1/auth/totp/verify', input),
    onMutate: () => {
      void qc.cancelQueries({ queryKey: authKeys.me, exact: true });
      return sessionGuard(qc);
    },
    onSuccess: (me, _input, isCurrent) => {
      if (!isCurrent()) return;
      forgetSession(qc, true);
      qc.setQueryData(authKeys.me, me);
    },
  });
};

export const useTotpSetup = () => useMutation({ mutationFn: () => api.post<TotpSetup>('/api/v1/auth/totp/setup') });

export const useTotpConfirm = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { password: string; code: string }) => api.post<TotpEnrolment>('/api/v1/auth/totp/confirm', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: authKeys.me }),
  });
};

export const useTotpDisable = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: SecondFactor & { password: string }) => api.post<void>('/api/v1/auth/totp/disable', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: authKeys.me }),
  });
};

export const useLogout = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<void>('/api/v1/auth/logout'),
    onMutate: () => sessionGuard(qc),
    onSuccess: (_data, _input, isCurrent) => {
      if (isCurrent()) forgetSession(qc);
    },
  });
};

export const useChangePassword = () =>
  useMutation({
    mutationFn: (input: { current: string; new: string }) => api.post<void>('/api/v1/me/password', input),
  });

export const useAdmins = () =>
  useQuery({ queryKey: authKeys.admins, queryFn: () => api.get<{ items: Admin[]; total: number }>('/api/v1/admins') });

export const useCreateAdmin = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { username: string; password: string; role: string }) => api.post<Admin>('/api/v1/admins', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: authKeys.admins }),
  });
};

export const useDeleteAdmin = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/api/v1/admins/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: authKeys.admins }),
  });
};
