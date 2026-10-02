import { Plus, RefreshCw, Server } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';

import { useFleetRollouts, useNodes } from '@/api/nodes';
import { useUpdateStatus } from '@/api/updates';
import { useAuth } from '@/auth/AuthProvider';
import { DataTableSkeleton } from '@/components/common/DataTable';
import { EmptyState } from '@/components/common/EmptyState';
import { PageHeader } from '@/components/common/PageHeader';
import { Panel } from '@/components/common/Panel';
import { RowChevron } from '@/components/common/RowChevron';
import { ServerOrderList } from '@/components/common/ServerOrder';
import { DragHandle } from '@/components/common/Sortable';
import { useSortableItem } from '@/components/common/sortableItem';
import { StatusBadge } from '@/components/common/StatusBadge';
import { Button } from '@/components/ui/button';
import { ENTER_CLASS } from '@/components/ui/motion';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { HelpButton } from '@/help';
import { formatCompactAge } from '@/lib/format';
import { useMediaQuery } from '@/lib/useMediaQuery';
import { cn } from '@/lib/utils';

import { CreateNodeDialog } from './CreateNodeDialog';
import { FleetRolloutStatus, FleetUpdateDialog } from './FleetUpdates';
import { InstallCommandDialog } from './InstallCommandDialog';
import { NodePeople } from './NodePeople';
import { NodeVersions } from './NodeVersions';
import { DASH, LOAD_TONE_CLASS, loadTone, nodeLoad, nodeStatus } from './nodeDisplay';

import type { MouseEvent, ReactNode } from 'react';
import type { CreateNodeResult, Node } from '@/api/types';

function LoadBar({ percent }: { percent: number | undefined }) {
  if (percent === undefined) return <span className="mono text-mono text-dim">{DASH}</span>;
  const tone = loadTone(percent);
  const classes = LOAD_TONE_CLASS[tone];
  return (
    <div className="w-14" data-testid="load-bar" data-tone={tone}>
      <span className={cn('mono block text-mono', classes.text)}>{Math.round(percent)}%</span>
      <div className="mt-1 h-[3px] w-full overflow-hidden rounded-pill bg-hairline">
        <div className={cn('h-full', classes.bar)} style={{ width: `${percent}%` }} />
      </div>
    </div>
  );
}

/** The same figure as text only, for the narrow layout where a rule per field would be noise. */
function LoadText({ percent }: { percent: number | undefined }) {
  if (percent === undefined) return <span className="mono text-dim">{DASH}</span>;
  return <span className={cn('mono', LOAD_TONE_CLASS[loadTone(percent)].text)}>{Math.round(percent)}%</span>;
}

function useHeartbeat(node: Node) {
  const { t, i18n } = useTranslation();
  const age = formatCompactAge(node.last_seen_at, i18n.language);
  return {
    offline: nodeStatus(node) === 'offline',
    text: age ? t('common.ago', { value: age }) : t('nodes.last_seen_never'),
  };
}

/** Anything inside a row that handles its own click, so opening the server does not fire too. */
const interactiveSelector = 'a, button, [role="checkbox"]';

function useOpenNode(node: Node) {
  const navigate = useNavigate();
  return (e: MouseEvent<HTMLElement>) => {
    if ((e.target as HTMLElement).closest(interactiveSelector)) return;
    navigate(`/nodes/${node.id}`);
  };
}

function NameCell({ node }: { node: Node }) {
  return (
    <>
      <Link
        to={`/nodes/${node.id}`}
        className="flex w-fit max-w-full items-center gap-2 font-medium text-foreground outline-none"
      >
        <StatusBadge status={nodeStatus(node)} hideLabel />
        <span className="truncate">{node.name}</span>
      </Link>
      <span className="mono block truncate pl-[15px] text-mono text-mute">{node.hostname}</span>
    </>
  );
}

interface Versions {
  pinnedTelemt?: string;
  panelVersion?: string;
}

function NodeTableRow({ node, writer, versions }: { node: Node; writer: boolean; versions: Versions }) {
  const { t } = useTranslation();
  const { setNode, style, dragging, handle } = useSortableItem(node.id, !writer);
  const { activator, attributes, listeners } = handle;
  const heartbeat = useHeartbeat(node);
  const load = nodeLoad(node);
  const open = useOpenNode(node);

  return (
    <TableRow interactive ref={setNode} style={style} onClick={open} className={cn(dragging && 'bg-elevated shadow-popover')}>
      {writer && (
        <TableCell className="w-0 pr-0">
          <DragHandle
            activator={activator}
            attributes={attributes}
            listeners={listeners}
            label={t('nodes.reorder_handle', { name: node.name })}
          />
        </TableCell>
      )}
      <TableCell className="max-w-72">
        <NameCell node={node} />
      </TableCell>
      <TableCell>
        <NodePeople node={node} />
      </TableCell>
      <TableCell>
        <LoadBar percent={load?.cpu} />
      </TableCell>
      <TableCell>
        <LoadBar percent={load?.mem} />
      </TableCell>
      <TableCell>
        <NodeVersions node={node} {...versions} />
      </TableCell>
      <TableCell className={cn('mono text-right text-mono', heartbeat.offline ? 'text-err' : 'text-mute')}>
        {heartbeat.text}
      </TableCell>
      <TableCell className="w-0 text-right">
        <RowChevron />
      </TableCell>
    </TableRow>
  );
}

/** Label above value, for the narrow layout where there is no column header to carry the name. */
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="micro truncate text-mute">{label}</dt>
      <dd className="mt-1 min-w-0 text-mono">{children}</dd>
    </div>
  );
}

function NodeCard({ node, writer, versions }: { node: Node; writer: boolean; versions: Versions }) {
  const { t } = useTranslation();
  const { setNode, style, dragging, handle } = useSortableItem(node.id, !writer);
  const { activator, attributes, listeners } = handle;
  const heartbeat = useHeartbeat(node);
  const load = nodeLoad(node);
  const open = useOpenNode(node);

  return (
    <li
      ref={setNode}
      style={style}
      onClick={open}
      className={cn(
        'group/row cursor-pointer px-4 py-3 transition-colors hover:bg-elevated focus-within:bg-elevated',
        dragging && 'bg-elevated shadow-popover',
      )}
    >
      <div className="flex items-start gap-2">
        {writer && (
          <DragHandle
            activator={activator}
            attributes={attributes}
            listeners={listeners}
            label={t('nodes.reorder_handle', { name: node.name })}
            className="-my-1 -ml-2"
          />
        )}
        <div className="min-w-0 flex-1">
          <NameCell node={node} />
        </div>
        <RowChevron className="mt-1" />
      </div>
      <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2.5">
        <Field label={t('common.people_online')}>
          <NodePeople node={node} />
        </Field>
        <Field label={t('nodes.column_heartbeat')}>
          <span className={cn('mono', heartbeat.offline ? 'text-err' : 'text-mute')}>{heartbeat.text}</span>
        </Field>
        <Field label={t('nodes.load_cpu')}>
          <LoadText percent={load?.cpu} />
        </Field>
        <Field label={t('nodes.load_ram')}>
          <LoadText percent={load?.mem} />
        </Field>
        <div className="col-span-2">
          <Field label={t('nodes.column_versions')}>
            <NodeVersions node={node} {...versions} />
          </Field>
        </div>
      </dl>
    </li>
  );
}

interface InstallResult {
  command: string;
  expires_at: string;
  nodeId: string;
}

// The fleet, in the order every list in the panel follows.
export function NodesPage() {
  const { t } = useTranslation();
  const { isWriter } = useAuth();
  const { data, isLoading } = useNodes();
  const rollouts = useFleetRollouts();
  const updateStatus = useUpdateStatus();
  const narrow = useMediaQuery('(max-width: 767.98px)');

  const [createOpen, setCreateOpen] = useState(false);
  const [updateOpen, setUpdateOpen] = useState(false);
  const [installResult, setInstallResult] = useState<InstallResult | null>(null);

  const nodes = data?.items ?? [];
  const versions: Versions = { pinnedTelemt: rollouts.data?.version, panelVersion: updateStatus.data?.current };
  const telemt = nodes.some((n) => n.engine === 'telemt');
  const latest = rollouts.data?.items[0];
  const shownRollout = latest && (latest.status === 'running' || latest.status === 'failed') ? latest : undefined;

  const handleCreated = (result: CreateNodeResult) => {
    setInstallResult({ command: result.install_command, expires_at: result.expires_at, nodeId: result.node.id });
  };

  const addButton = isWriter && (
    <Button type="button" onClick={() => setCreateOpen(true)}>
      <Plus />
      {t('nodes.add')}
    </Button>
  );

  return (
    <>
      <div className="space-y-6">
        <PageHeader
          title={t('nodes.title')}
          description={t('workspace.fleet_hint')}
          actions={
            <>
              <HelpButton topic="nodes.list" />
              {isWriter && telemt && (
                <Button type="button" variant="outline" onClick={() => setUpdateOpen(true)}>
                  <RefreshCw />
                  {t('nodes.fleet_open')}
                </Button>
              )}
              {addButton}
            </>
          }
        />

        {shownRollout && <FleetRolloutStatus rollout={shownRollout} nodes={nodes} writer={isWriter} />}
        {isLoading ? (
          <DataTableSkeleton columns={isWriter ? 7 : 6} rows={4} />
        ) : nodes.length === 0 ? (
          <EmptyState
            icon={Server}
            title={t('nodes.empty_title')}
            description={t('nodes.empty_description')}
            action={addButton}
          />
        ) : (
          <ServerOrderList servers={nodes}>
            {isWriter && nodes.length > 1 && (
              <p className="-mt-3 text-label text-mute">{t(narrow ? 'nodes.reorder_hint_cards' : 'nodes.reorder_hint_rows')}</p>
            )}
            <Panel className={ENTER_CLASS}>
              {narrow ? (
                <ul className="divide-y divide-hairline">
                  {nodes.map((node) => (
                    <NodeCard key={node.id} node={node} writer={isWriter} versions={versions} />
                  ))}
                </ul>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      {isWriter && (
                        <TableHead className="w-0">
                          <span className="sr-only">{t('nodes.column_order')}</span>
                        </TableHead>
                      )}
                      <TableHead>{t('nodes.column_name')}</TableHead>
                      <TableHead>{t('common.people_online')}</TableHead>
                      <TableHead>{t('nodes.load_cpu')}</TableHead>
                      <TableHead>{t('nodes.load_ram')}</TableHead>
                      <TableHead>{t('nodes.column_versions')}</TableHead>
                      <TableHead className="text-right">{t('nodes.column_heartbeat')}</TableHead>
                      <TableHead className="w-0" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {nodes.map((node) => (
                      <NodeTableRow key={node.id} node={node} writer={isWriter} versions={versions} />
                    ))}
                  </TableBody>
                </Table>
              )}
            </Panel>
          </ServerOrderList>
        )}
      </div>

      <CreateNodeDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={handleCreated} />
      {isWriter && telemt && <FleetUpdateDialog open={updateOpen} onOpenChange={setUpdateOpen} nodes={nodes} />}

      {installResult && (
        <InstallCommandDialog
          open={!!installResult}
          onOpenChange={(open) => !open && setInstallResult(null)}
          nodeId={installResult.nodeId}
          command={installResult.command}
          expiresAt={installResult.expires_at}
          regenerated={false}
        />
      )}
    </>
  );
}
