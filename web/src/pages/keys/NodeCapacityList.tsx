import { useTranslation } from 'react-i18next';

import { Checkbox } from '@/components/ui/checkbox';
import { cn } from '@/lib/utils';

import { capacityText } from '@/pages/nodes/nodeDisplay';

import type { Node } from '@/api/types';

interface NodeCapacityListProps {
  nodes: Node[];
  selectedIds: string[];
  onChange: (next: string[]) => void;
  className?: string;
}

/** True once a node's profile count has reached its configured cap (0 = unlimited). */
export function isNodeFull(node: Node): boolean {
  return node.max_profiles > 0 && node.profile_count >= node.max_profiles;
}

/**
 * Where the key will live.
 *
 * A list rather than a multi-select popup: which nodes a key binds to is the
 * decision this dialog exists for, and it is made against each node's
 * remaining capacity - so the count sits on every row, in mono, and a node
 * with no room left is struck out and unselectable rather than silently
 * failing on submit.
 */
export function NodeCapacityList({ nodes, selectedIds, onChange, className }: NodeCapacityListProps) {
  const { t } = useTranslation();

  const toggle = (id: string, checked: boolean) => {
    onChange(checked ? [...selectedIds, id] : selectedIds.filter((x) => x !== id));
  };

  if (nodes.length === 0) {
    return (
      <p className="rounded-md border border-hairline px-3 py-4 text-center text-sm text-mute">{t('keys.no_nodes')}</p>
    );
  }

  return (
    <div className={cn('max-h-52 divide-y divide-hairline overflow-y-auto rounded-md border border-hairline', className)}>
      {nodes.map((node) => {
        const checked = selectedIds.includes(node.id);
        const full = isNodeFull(node) && !checked;
        return (
          <label
            key={node.id}
            className={cn(
              'flex cursor-pointer items-center justify-between gap-3 px-3 py-2 transition-colors hover:bg-elevated',
              full && 'cursor-not-allowed opacity-45 hover:bg-transparent',
            )}
          >
            <span className="flex min-w-0 items-center gap-2.5">
              <Checkbox checked={checked} disabled={full} onCheckedChange={(v) => toggle(node.id, !!v)} />
              <span className="min-w-0">
                <span className="block truncate text-sm text-foreground">{node.name}</span>
                <span className="mono block truncate text-xs text-dim">{node.hostname}</span>
              </span>
            </span>
            <span className={cn('mono shrink-0 text-xs', full ? 'text-err' : 'text-mute')}>
              {capacityText(node.profile_count, node.max_profiles)}
            </span>
          </label>
        );
      })}
    </div>
  );
}
