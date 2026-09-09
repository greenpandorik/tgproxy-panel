import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/lib/api';

import type { Admin, LoginResult, Me, SecondFactor, TotpEnrolment, TotpSetup } from './types';
import { isTotpChallenge } from './types';

export const authKeys = {
  me: ['auth', 'me'] as const,
  admins: ['auth', 'admins'] as const,
};

export const useMe = () => useQuery({ queryKey: authKeys.me, queryFn: () => api.get<Me>('/api/v1/auth/me'), retry: false });

export const useLogin = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { username: string; password: string }) => api.post<LoginResult>('/api/v1/auth/login', input),
    onSuccess: (res) => {
      if (!isTotpChallenge(res)) qc.setQueryData(authKeys.me, res);
    },
  });
};

/** Second step of login: exchanges the challenge for a session. */
export const useTotpVerify = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: SecondFactor & { challenge: string }) => api.post<Me>('/api/v1/auth/totp/verify', input),
    onSuccess: (me) => qc.setQueryData(authKeys.me, me),
  });
};

export const useTotpSetup = () => useMutation({ mutationFn: () => api.post<TotpSetup>('/api/v1/auth/totp/setup') });

export const useTotpConfirm = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { password: string; code: string }) =>
      api.post<TotpEnrolment>('/api/v1/auth/totp/confirm', input),
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
    onSuccess: () => qc.setQueryData(authKeys.me, null),
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
