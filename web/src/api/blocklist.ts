import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/lib/api';
import { sessionGuard } from '@/lib/session';

export interface BlocklistEntry {
  prefix: string;
  note: string;
  added_at: string;
  packets: number | null;
  bytes: number | null;
}

export interface Blocklist {
  entries: BlocklistEntry[];
  revision: number;
  updated_at: string | null;
  max_entries: number;
  supported: boolean | null;
  live: boolean;
  synced: boolean;
  node_revision: number | null;
  node_entries: number | null;
  dropped_packets: number | null;
  dropped_bytes: number | null;
  node_error: string;
  apply_error?: string;
}

export interface BlocklistEntryInput {
  prefix: string;
  note?: string;
}

export const blocklistKey = (id: string) => ['nodes', id, 'blocklist'] as const;

export const useBlocklist = (id: string) =>
  useQuery({
    queryKey: blocklistKey(id),
    queryFn: () => api.get<Blocklist>(`/api/v1/nodes/${id}/blocklist`),
    enabled: !!id,
    refetchInterval: 30_000,
  });

export function useSaveBlocklist(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ revision, entries }: { revision: number; entries: BlocklistEntryInput[] }) =>
      api.put<Blocklist>(`/api/v1/nodes/${id}/blocklist`, { revision, entries }),
    onMutate: () => sessionGuard(qc),
    onSuccess: (data, _input, isCurrent) => {
      if (isCurrent()) qc.setQueryData(blocklistKey(id), data);
    },
    onError: (_error, _input, isCurrent) => {
      if (isCurrent?.()) void qc.invalidateQueries({ queryKey: blocklistKey(id) });
    },
  });
}
