import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';

import { useReorderNodes } from '@/api/nodes';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';

import { SortableList } from './Sortable';

import type { SortableMessages } from './Sortable';
import type { ReactNode } from 'react';

/** The words a screen reader hears while a server moves. */
function useServerOrderMessages(): SortableMessages {
  const { t } = useTranslation();
  return useMemo(
    () => ({
      instructions: t('nodes.reorder_instructions'),
      picked: (name, position, total) => t('nodes.reorder_picked', { name, position, total }),
      moved: (name, position, total) => t('nodes.reorder_moved', { name, position, total }),
      dropped: (name, position, total) => t('nodes.reorder_dropped', { name, position, total }),
      cancelled: (name) => t('nodes.reorder_cancelled', { name }),
    }),
    [t],
  );
}

/** A list of servers in the shared order, which a writer reorders by dragging and every other list follows. */
export function ServerOrderList({
  servers,
  layout,
  children,
}: {
  servers: { id: string; name: string }[];
  layout?: 'list' | 'grid';
  children: ReactNode;
}) {
  const { t } = useTranslation();
  const reorder = useReorderNodes();
  const messages = useServerOrderMessages();
  const ids = servers.map((s) => s.id);
  const names = new Map(servers.map((s) => [s.id, s.name]));

  const save = (next: string[]) =>
    reorder.mutate(next, {
      onError: (err) => {
        const detail = err instanceof ApiError ? err.message : t('common.error_generic');
        toast.add({ title: t('nodes.reorder_failed'), description: detail, type: 'error' });
      },
    });

  return (
    <SortableList items={ids} onReorder={save} layout={layout} nameOf={(id) => names.get(id) ?? id} messages={messages}>
      {children}
    </SortableList>
  );
}
