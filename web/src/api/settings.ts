import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/lib/api';

import type { PutSettingsInput, Settings, TelegramTestInput, TelegramTestResult } from './types';

export const settingsKeys = {
  all: ['settings'] as const,
};

export const useSettings = () => useQuery({ queryKey: settingsKeys.all, queryFn: () => api.get<Settings>('/api/v1/settings') });

export const usePutSettings = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: PutSettingsInput) => api.put<Settings>('/api/v1/settings', b),
    onSuccess: () => qc.invalidateQueries({ queryKey: settingsKeys.all }),
  });
};

export const useTelegramTest = () =>
  useMutation({
    mutationFn: (b: TelegramTestInput) => api.post<TelegramTestResult>('/api/v1/settings/telegram/test', b),
  });
