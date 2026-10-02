import { Search } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { PanelEmpty } from '@/components/common/EmptyState';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
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

// Where the key will live.
export function NodeCapacityList({ nodes, selectedIds, onChange, className }: NodeCapacityListProps) {
  const { t } = useTranslation();
  const [query, setQuery] = useState('');

  const toggle = (id: string, checked: boolean) => {
    onChange(checked ? [...selectedIds, id] : selectedIds.filter((x) => x !== id));
  };

  if (nodes.length === 0) {
    return <PanelEmpty className="rounded-control border border-hairline">{t('keys.no_nodes')}</PanelEmpty>;
  }

  const needle = query.trim().toLowerCase();
  const shown = needle
    ? nodes.filter((n) => n.name.toLowerCase().includes(needle) || n.hostname.toLowerCase().includes(needle))
    : nodes;

  return (
    <div className={cn('space-y-2', className)}>
      {nodes.length > 5 && (
        <div className="flex items-center gap-3">
          <div className="relative min-w-0 flex-1">
            <Search
              size={14}
              className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-mute"
              aria-hidden="true"
            />
            <Input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('keys.nodes_search')}
              aria-label={t('keys.nodes_search')}
              className="pl-8"
            />
          </div>
          <span className="mono shrink-0 text-mono text-mute">
            {t('keys.nodes_selected', { count: selectedIds.length, total: nodes.length })}
          </span>
        </div>
      )}
      <div className="max-h-[min(30rem,50vh)] divide-y divide-hairline overflow-y-auto rounded-control border border-hairline">
        {shown.length === 0 && <p className="px-3 py-4 text-body text-mute">{t('keys.nodes_search_empty')}</p>}
        {shown.map((node) => {
          const checked = selectedIds.includes(node.id);
          const full = isNodeFull(node) && !checked;
          return (
            <label
              key={node.id}
              className={cn(
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
    </div>
  );
}
