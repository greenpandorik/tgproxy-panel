import { ChevronRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { isMetricPresent } from '@/components/common/metric';
import { ServerOrderList } from '@/components/common/ServerOrder';
import { DragHandle } from '@/components/common/Sortable';
import { useSortableItem } from '@/components/common/sortableItem';
import { StatusBadge } from '@/components/common/StatusBadge';
import { formatCompactAge, formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';
import { DC_TONE_TEXT, dcTone, nodeDcLatency } from '@/pages/nodes/dcDisplay';
import { LOAD_TONE_CLASS, loadTone, nodeLoad } from '@/pages/nodes/nodeDisplay';

import type { Node } from '@/api/types';

/** A thin bar for a load figure; an empty track when there is no figure. */
function LoadLine({ percent }: { percent: number | undefined }) {
  return (
    <span className="block h-1 overflow-hidden rounded-pill bg-hairline" aria-hidden="true">
      {percent !== undefined && (
        <span className={cn('block h-full', LOAD_TONE_CLASS[loadTone(percent)].bar)} style={{ width: `${percent}%` }} />
      )}
    </span>
  );
}

function ServerCard({ node, writer }: { node: Node; writer: boolean }) {
  const { t, i18n } = useTranslation();
  const { setNode, style, dragging, handle } = useSortableItem(node.id, !writer);
  const { activator, attributes, listeners } = handle;
  const num = (v: number) => formatNumber(v, i18n.language);
  const age = formatCompactAge(node.last_seen_at, i18n.language);
  const offline = node.status === 'offline';
  const load = nodeLoad(node);
  const latency = nodeDcLatency(node);

  const facts = [
    load?.cpu !== undefined ? t('dashboard.card_cpu', { value: Math.round(load.cpu) }) : null,
    load ? t('dashboard.card_ram', { value: Math.round(load.mem) }) : null,
    age ? t('common.ago', { value: age }) : null,
  ].filter(Boolean);

  return (
    <li
      ref={setNode}
      style={style}
      className={cn(
        'group/card relative flex min-w-0 flex-col gap-2 rounded-surface border bg-card px-4 py-3 transition-[background-color,border-color] hover:bg-elevated/40 focus-within:border-brand-primary/60',
        offline ? 'border-err/50 hover:border-err/70' : 'border-hairline-strong hover:border-brand-primary/50',
        dragging && 'border-brand-primary/60 shadow-popover',
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        {writer && (
          <DragHandle
            activator={activator}
            attributes={attributes}
            listeners={listeners}
            label={t('nodes.reorder_handle', { name: node.name })}
            className="-my-1 -ml-2"
          />
        )}
        <StatusBadge status={node.status} hideLabel />
        <Link
          to={`/nodes/${node.id}`}
          className="min-w-0 flex-1 truncate text-body font-semibold text-foreground outline-none after:absolute after:inset-0 after:rounded-surface focus-visible:after:ring-2 focus-visible:after:ring-ring"
        >
          {node.name}
        </Link>
        <ChevronRight className="size-4 shrink-0 text-mute transition-colors group-hover/card:text-primary" aria-hidden="true" />
      </div>
      <p className="mono -mt-1 truncate text-mono text-mute">{node.hostname}</p>

      {offline || node.status === 'pending' ? (
        <p className={cn('text-body', offline ? 'text-err' : 'text-mute')}>
          {offline
            ? age
              ? t('dashboard.card_offline_for', { value: age })
              : t('dashboard.card_offline')
            : t('nodes.last_seen_never')}
        </p>
      ) : (
        <p className="flex items-baseline justify-between gap-3 text-body text-foreground">
          <span>
            {isMetricPresent(node.people_online)
              ? t('dashboard.card_people', { value: num(node.people_online) })
              : t('dashboard.card_people_unknown')}
          </span>
          {latency !== undefined && (
            <span className={cn('mono text-mono', DC_TONE_TEXT[dcTone(latency)])} title={t('nodes.column_telegram')}>
              {num(Math.round(latency))} {t('common.ms')}
            </span>
          )}
        </p>
      )}

      <div className="space-y-1.5">
        <LoadLine percent={load?.cpu} />
        <LoadLine percent={load?.mem} />
      </div>
      <p className="mono truncate text-micro text-mute">
        {offline || node.status === 'pending' || facts.length === 0 ? t('dashboard.card_no_data') : facts.join(' · ')}
      </p>
    </li>
  );
}

/** The servers as cards, in the shared order; a writer drags a card by its grip to move it. */
export function ServerCards({ nodes, writer }: { nodes: Node[]; writer: boolean }) {
  return (
    <ServerOrderList servers={nodes} layout="grid">
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,260px),1fr))] gap-3">
        {nodes.map((node) => (
          <ServerCard key={node.id} node={node} writer={writer} />
        ))}
      </ul>
    </ServerOrderList>
  );
}
