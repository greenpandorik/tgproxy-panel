import { useTranslation } from 'react-i18next';

import { PanelEmpty } from '@/components/common/EmptyState';
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
    return <PanelEmpty className="rounded-control border border-hairline">{t('keys.no_nodes')}</PanelEmpty>;
  }

  return (
    <div className={cn('max-h-52 divide-y divide-hairline overflow-y-auto rounded-control border border-hairline', className)}>
      {nodes.map((node) => {
        const checked = selectedIds.includes(node.id);
        const full = isNodeFull(node) && !checked;
        return (
          <label
            key={node.id}
            className={cn(
              // The whole row is the target, so the whole row presses: the same
              // sub-pixel squeeze every button in the panel carries, which is
              // what tells a pointer the click landed on a control with no
              // background of its own.
              'flex cursor-pointer items-center justify-between gap-4 px-3 py-2 transition-[background-color,scale] hover:bg-elevated active:scale-[0.985]',
              full && 'cursor-not-allowed opacity-45 hover:bg-transparent active:scale-100',
            )}
          >
            <span className="flex min-w-0 items-center gap-3">
              <Checkbox checked={checked} disabled={full} onCheckedChange={(v) => toggle(node.id, !!v)} />
              <span className="min-w-0">
                <span className="block truncate text-body text-foreground">{node.name}</span>
                <span className="mono block truncate text-mono text-mute">{node.hostname}</span>
              </span>
            </span>
            <span className={cn('mono shrink-0 text-mono', full ? 'text-err' : 'text-mute')}>
              {capacityText(node.profile_count, node.max_profiles)}
            </span>
          </label>
        );
      })}
    </div>
  );
}
