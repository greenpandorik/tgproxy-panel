import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { StatusBadge } from '@/components/common/StatusBadge';
import { Button } from '@/components/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatCompactAge, formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';
import { capacityText, LOAD_TONE_CLASS, loadTone, nodeLoad } from '@/pages/nodes/nodeDisplay';

import type { ReactNode } from 'react';
import type { Status } from '@/components/common/StatusBadge';
import type { Node } from '@/api/types';

const DASH = '—';

interface NodesTableProps {
  nodes: Node[];
  /** Live sessions per node id, taken from the newest monitoring sample. */
  sessionsByNode: Record<string, number | undefined>;
}

/** What the machine reports about one node, already formatted and toned. */
interface NodeRow {
  offline: boolean;
  relay: string;
  relayTone: string;
  profiles: string;
  cpu: string;
  cpuTone: string;
  sessions: string;
  sessionsTone: string;
  heartbeat: string;
}

function useNodeRow(node: Node, sessions: number | undefined): NodeRow {
  const { t, i18n } = useTranslation();
  const age = formatCompactAge(node.last_seen_at, i18n.language);
  const load = nodeLoad(node);
  return {
    offline: node.status === 'offline',
    relay: node.tproxy_version || DASH,
    relayTone: node.tproxy_version ? 'text-mute' : 'text-dim',
    profiles: capacityText(node.profile_count, node.max_profiles),
    cpu: load ? `${Math.round(load.cpu)}%` : DASH,
    cpuTone: load ? LOAD_TONE_CLASS[loadTone(load.cpu)].text : 'text-dim',
    sessions: sessions === undefined ? DASH : formatNumber(sessions, i18n.language),
    sessionsTone: sessions === undefined ? 'text-dim' : 'text-foreground',
    heartbeat: age ? t('common.ago', { value: age }) : t('nodes.last_seen_never'),
  };
}

/** Label above value, for the narrow layout where there are no column headers to carry the name. */
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="truncate text-[11px] text-dim">{label}</dt>
      <dd className="mono mt-0.5 truncate text-xs">{children}</dd>
    </div>
  );
}

function NodeCard({ node, sessions }: { node: Node; sessions: number | undefined }) {
  const { t } = useTranslation();
  const row = useNodeRow(node, sessions);

  return (
    <li className="px-4 py-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="flex items-center gap-2 text-sm font-medium text-foreground">
            <StatusBadge status={node.status as Status} hideLabel />
            <span className="truncate">{node.name}</span>
          </p>
          <p className="mono mt-0.5 truncate pl-[15px] text-xs text-mute">{node.hostname}</p>
        </div>
        <Button variant="outline" size="sm" className="shrink-0" render={<Link to={`/nodes/${node.id}`} />}>
          {t('nodes.action_open')}
        </Button>
      </div>

      <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-5">
        <Field label={t('dashboard.col_relay')}>
          <span className={row.relayTone}>{row.relay}</span>
        </Field>
        <Field label={t('nodes.column_profiles')}>
          <span className="text-mute">{row.profiles}</span>
        </Field>
        <Field label={t('nodes.load_cpu')}>
          <span className={row.cpuTone}>{row.cpu}</span>
        </Field>
        <Field label={t('dashboard.col_sessions')}>
          <span className={row.sessionsTone}>{row.sessions}</span>
        </Field>
        <Field label={t('dashboard.col_heartbeat')}>
          <span className={row.offline ? 'text-err' : 'text-mute'}>{row.heartbeat}</span>
        </Field>
      </dl>
    </li>
  );
}

function NodeTableRow({ node, sessions }: { node: Node; sessions: number | undefined }) {
  const { t } = useTranslation();
  const row = useNodeRow(node, sessions);

  return (
    <TableRow>
      <TableCell className="font-medium text-foreground">
        <span className="inline-flex items-center gap-2">
          <StatusBadge status={node.status as Status} hideLabel />
          <span className="truncate">{node.name}</span>
        </span>
      </TableCell>
      <TableCell className="mono text-xs text-mute">{node.hostname}</TableCell>
      <TableCell className={cn('mono text-xs', row.relayTone)}>{row.relay}</TableCell>
      <TableCell className="mono text-xs text-mute">{row.profiles}</TableCell>
      <TableCell className={cn('mono text-right text-xs', row.cpuTone)}>{row.cpu}</TableCell>
      <TableCell className={cn('mono text-right text-xs', row.sessionsTone)}>{row.sessions}</TableCell>
      <TableCell className={cn('mono text-right text-xs', row.offline ? 'text-err' : 'text-mute')}>{row.heartbeat}</TableCell>
      <TableCell className="text-right">
        <Button variant="outline" size="sm" render={<Link to={`/nodes/${node.id}`} />}>
          {t('nodes.action_open')}
        </Button>
      </TableCell>
    </TableRow>
  );
}

/**
 * The fleet, in two shapes for two widths.
 *
 * Wide: one table. Everything the machine reports - host, relay build, profile
 * use, CPU load, sessions, heartbeat - is mono, so the eye can run down a
 * column and spot the row that does not match its neighbours.
 *
 * Narrow: the same eight fields as a stacked row, because a table that has to
 * be scrolled sideways hides exactly the columns this panel exists to show -
 * sessions, heartbeat, and the way into the node. Column headers cannot
 * survive the fold, so each value carries its own label instead.
 *
 * Either way a node that has stopped reporting says so in red on its
 * heartbeat, the field that actually went wrong; the rest of it stays quiet.
 */
export function NodesTable({ nodes, sessionsByNode }: NodesTableProps) {
  const { t } = useTranslation();

  return (
    <>
      <div className="hidden md:block">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('dashboard.col_node')}</TableHead>
              <TableHead>{t('nodes.column_hostname')}</TableHead>
              <TableHead>{t('dashboard.col_relay')}</TableHead>
              <TableHead>{t('nodes.column_profiles')}</TableHead>
              <TableHead className="text-right">{t('nodes.load_cpu')}</TableHead>
              <TableHead className="text-right">{t('dashboard.col_sessions')}</TableHead>
              <TableHead className="text-right">{t('dashboard.col_heartbeat')}</TableHead>
              <TableHead className="w-0" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {nodes.map((node) => (
              <NodeTableRow key={node.id} node={node} sessions={sessionsByNode[node.id]} />
            ))}
          </TableBody>
        </Table>
      </div>

      <ul className="divide-y divide-hairline md:hidden">
        {nodes.map((node) => (
          <NodeCard key={node.id} node={node} sessions={sessionsByNode[node.id]} />
        ))}
      </ul>
    </>
  );
}
