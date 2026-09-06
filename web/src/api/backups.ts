import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/lib/api';

import type { Backup } from './types';

export const backupKeys = {
  all: ['backups'] as const,
};

export const useBackups = (enabled = true) =>
  useQuery({
    queryKey: backupKeys.all,
    queryFn: () => api.get<{ items: Backup[] }>('/api/v1/backups'),
    enabled,
  });

export const useCreateBackup = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<Backup>('/api/v1/backups'),
    onSuccess: () => qc.invalidateQueries({ queryKey: backupKeys.all }),
  });
};

export const useDeleteBackup = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/api/v1/backups/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: backupKeys.all }),
  });
};

/**
 * The download is a plain navigation rather than a fetch: the file is the whole
 * database and can be hundreds of megabytes, so it goes straight to disk through
 * the browser's own downloader (which sends the session cookie) instead of
 * through memory as a blob.
 */
export const backupDownloadURL = (id: string) => `/api/v1/backups/${id}/download`;
