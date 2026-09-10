import { MoreHorizontal, Plus, Server } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';

import { useApplyNode, useDeleteNode, useInstallCommand, useNodes } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { CopyButton } from '@/components/common/CopyButton';
import { DataTableSkeleton } from '@/components/common/DataTable';
import { EmptyState } from '@/components/common/EmptyState';
import { PageHeader } from '@/components/common/PageHeader';
import { Panel } from '@/components/common/Panel';
import { StatusBadge } from '@/components/common/StatusBadge';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import { ENTER_CLASS } from '@/components/ui/motion';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { formatCompactAge, formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';

import { CreateNodeDialog } from './CreateNodeDialog';
import { DC_TONE_TEXT, dcTone, nodeDcLatency } from './dcDisplay';
import { EngineTag } from './EngineTag';
import { InstallCommandDialog } from './InstallCommandDialog';
import { capacityText, DASH, engineVersion, LOAD_TONE_CLASS, loadTone, nodeLoad, nodeStatus, shortVersion } from './nodeDisplay';

import type { MouseEvent, ReactNode } from 'react';
import type { CreateNodeResult, Node } from '@/api/types';

function CapacityBar({ count, max }: { count: number; max: number }) {
  const capped = max > 0;
  const pct = capped ? Math.min(100, Math.round((count / max) * 100)) : 0;
  const tight = capped && pct >= 90;

  return (
    <div className="w-16">
      <span className={cn('mono block text-mono', tight ? 'text-err' : 'text-mute')}>{capacityText(count, max)}</span>
      {/* No cap configured means there is no proportion to draw; the rule is
          omitted rather than shown permanently empty. */}
      {capped && (
        <div className="mt-1 h-[3px] w-full overflow-hidden rounded-pill bg-hairline">
          <div className={cn('h-full', tight ? 'bg-err' : 'bg-brand-primary')} style={{ width: `${pct}%` }} />
        </div>
      )}
    </div>
  );
}

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

function DcLatencyText({ ms }: { ms: number | undefined }) {
  const { t, i18n } = useTranslation();
  if (ms === undefined) return <span className="mono text-mono text-dim">{DASH}</span>;
  const tone = dcTone(ms);
  return (
    <span className={cn('mono text-mono', DC_TONE_TEXT[tone])} data-testid="dc-latency" data-tone={tone}>
      {formatNumber(Math.round(ms), i18n.language)} {t('common.ms')}
    </span>
  );
}

/** The fields the machine reports about one node, formatted and toned once for both layouts. */
function useNodeRow(node: Node) {
  const { t, i18n } = useTranslation();
  const age = formatCompactAge(node.last_seen_at, i18n.language);
  const version = engineVersion(node);
  return {
    offline: nodeStatus(node) === 'offline',
    relay: shortVersion(version),
    relayTone: version ? 'text-mute' : 'text-dim',
    heartbeat: age ? t('common.ago', { value: age }) : t('nodes.last_seen_never'),
    load: nodeLoad(node),
    telegram: nodeDcLatency(node),
  };
}

function EngineCell({ node }: { node: Node }) {
  const row = useNodeRow(node);
  return (
    <span className="inline-flex items-center gap-1.5">
      <EngineTag engine={node.engine} />
      <span className={cn('mono text-mono', row.relayTone)}>{row.relay}</span>
    </span>
  );
}

/** The "not yet pushed" tag: the node's config differs from what the agent last applied. */
function DirtyTag({ dirty }: { dirty: boolean }) {
  const { t } = useTranslation();
  if (!dirty) return <span className="text-dim">{DASH}</span>;
  return <Badge variant="warn">{t('nodes.dirty_tag')}</Badge>;
}

/** Hostname with a copy affordance that stays out of the way until the row is pointed at. */
function HostCell({ hostname }: { hostname: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <span className="mono text-mono text-mute">{hostname}</span>
      <CopyButton
        value={hostname}
        className="size-6 opacity-0 transition-opacity group-hover/row:opacity-100 focus-visible:opacity-100 [&_svg]:size-3"
      />
    </span>
  );
}

interface RowActionsProps {
  node: Node;
  onShowInstall: (result: { command: string; expires_at: string }) => void;
  onDelete: (node: Node) => void;
}

function RowActions({ node, onShowInstall, onDelete }: RowActionsProps) {
  const { t } = useTranslation();
  const applyNode = useApplyNode(node.id);
  const installCommand = useInstallCommand(node.id);

  const handleApply = async () => {
    try {
      await applyNode.mutateAsync();
      toast.add({ description: t('nodes.apply_queued'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleInstall = async () => {
    try {
      const result = await installCommand.mutateAsync();
      onShowInstall(result);
    } catch {
      toast.add({ description: t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="icon-sm" />}>
        <MoreHorizontal />
        <span className="sr-only">{t('common.actions')}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={() => void handleApply()}>{t('nodes.action_apply_now')}</DropdownMenuItem>
        <DropdownMenuItem onClick={() => void handleInstall()}>{t('nodes.action_install_command')}</DropdownMenuItem>
        <DropdownMenuItem variant="destructive" onClick={() => onDelete(node)}>
          {t('nodes.action_delete')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** Label above value, for the narrow layout where there is no column header to carry the name. */
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="micro truncate text-mute">{label}</dt>
      {/* No `truncate` here: it would clip the border of a tag sitting in the slot. */}
      <dd className="mt-1 min-w-0 text-mono">{children}</dd>
    </div>
  );
}

// Anything inside a row that handles its own click, so opening the node does not fire too.
const interactiveSelector = 'a, button, [role="menuitem"], [role="menu"]';

function NodeCard({ node, actions }: { node: Node; actions: ReactNode }) {
  const { t } = useTranslation();
  const row = useNodeRow(node);
  const navigate = useNavigate();

  const openNode = (e: MouseEvent<HTMLLIElement>) => {
    if ((e.target as HTMLElement).closest(interactiveSelector)) return;
    navigate(`/nodes/${node.id}`);
  };

  return (
    <li className="cursor-pointer px-4 py-3" onClick={openNode}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <Link
            to={`/nodes/${node.id}`}
            className="flex items-center gap-2 text-body font-medium text-foreground hover:underline"
          >
            <StatusBadge status={nodeStatus(node)} hideLabel />
            <span className="truncate">{node.name}</span>
          </Link>
          <p className="mono mt-1 truncate pl-[15px] text-mono text-mute">{node.hostname}</p>
        </div>
        {actions}
      </div>

      <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2.5">
        <Field label={t('nodes.column_relay')}>
          <EngineCell node={node} />
        </Field>
        <Field label={t('nodes.column_profiles')}>
          <CapacityBar count={node.profile_count} max={node.max_profiles} />
        </Field>
        <Field label={t('nodes.load_cpu')}>
          <LoadText percent={row.load?.cpu} />
        </Field>
        <Field label={t('nodes.load_ram')}>
          <LoadText percent={row.load?.mem} />
        </Field>
        <Field label={t('nodes.column_heartbeat')}>
          <span className={cn('mono', row.offline ? 'text-err' : 'text-mute')}>{row.heartbeat}</span>
        </Field>
        <Field label={t('nodes.column_changes')}>
          <DirtyTag dirty={node.dirty} />
        </Field>
      </dl>
    </li>
  );
}

function NodeTableRow({ node, actions }: { node: Node; actions: ReactNode }) {
  const row = useNodeRow(node);
  const navigate = useNavigate();

  const openNode = (e: MouseEvent<HTMLTableRowElement>) => {
    if ((e.target as HTMLElement).closest(interactiveSelector)) return;
    navigate(`/nodes/${node.id}`);
  };

  return (
    <TableRow className="group/row cursor-pointer" onClick={openNode}>
      <TableCell className="font-medium text-foreground">
        <Link to={`/nodes/${node.id}`} className="inline-flex items-center gap-2 hover:underline">
          <StatusBadge status={nodeStatus(node)} hideLabel />
          <span className="truncate">{node.name}</span>
        </Link>
      </TableCell>
      <TableCell>
        <HostCell hostname={node.hostname} />
      </TableCell>
      <TableCell>
        <EngineCell node={node} />
      </TableCell>
      <TableCell>
        <CapacityBar count={node.profile_count} max={node.max_profiles} />
      </TableCell>
      <TableCell>
        <LoadBar percent={row.load?.cpu} />
      </TableCell>
      <TableCell>
        <LoadBar percent={row.load?.mem} />
      </TableCell>
      <TableCell className="text-right">
        <DcLatencyText ms={row.telegram} />
      </TableCell>
      <TableCell className={cn('mono text-right text-mono', row.offline ? 'text-err' : 'text-mute')}>{row.heartbeat}</TableCell>
      <TableCell className="text-right">
        <DirtyTag dirty={node.dirty} />
      </TableCell>
      {actions && <TableCell className="w-0 text-right">{actions}</TableCell>}
    </TableRow>
  );
}

// The fleet.
export function NodesPage() {
  const { t } = useTranslation();
  const { isWriter } = useAuth();
  const { data, isLoading } = useNodes();
  const deleteNode = useDeleteNode();

  const [createOpen, setCreateOpen] = useState(false);
  const [installResult, setInstallResult] = useState<{ command: string; expires_at: string; nodeId?: string } | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Node | null>(null);

  const nodes = data?.items ?? [];
  const online = nodes.filter((n) => nodeStatus(n) === 'online').length;

  const handleCreated = (result: CreateNodeResult) => {
    setInstallResult({ command: result.install_command, expires_at: result.expires_at, nodeId: result.node.id });
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await deleteNode.mutateAsync(deleteTarget.id);
      toast.add({ description: t('nodes.delete_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const rowActions = (node: Node) =>
    isWriter ? <RowActions node={node} onShowInstall={setInstallResult} onDelete={setDeleteTarget} /> : null;

  return (
    <>
      <div className="space-y-6">
        <PageHeader
          title={t('nodes.title')}
          description={nodes.length > 0 ? t('nodes.header_online', { online, total: nodes.length }) : undefined}
          actions={
            <>
              <HelpButton topic="nodes.list" />
              {isWriter && (
                <Button type="button" onClick={() => setCreateOpen(true)}>
                  <Plus />
                  {t('nodes.add')}
                </Button>
              )}
            </>
          }
        />

        {isLoading ? (
          <DataTableSkeleton columns={isWriter ? 10 : 9} rows={4} />
        ) : nodes.length === 0 ? (
          <EmptyState
            icon={Server}
            title={t('nodes.empty_title')}
            description={t('nodes.empty_description')}
            action={
              isWriter && (
                <Button type="button" onClick={() => setCreateOpen(true)}>
                  <Plus />
                  {t('nodes.add')}
                </Button>
              )
            }
          />
        ) : (
          <Panel className={ENTER_CLASS}>
            <div className="hidden md:block">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('nodes.column_name')}</TableHead>
                    <TableHead>{t('nodes.column_hostname')}</TableHead>
                    <TableHead>{t('nodes.column_relay')}</TableHead>
                    <TableHead>{t('nodes.column_profiles')}</TableHead>
                    <TableHead>{t('nodes.load_cpu')}</TableHead>
                    <TableHead>{t('nodes.load_ram')}</TableHead>
                    {/* Wide layout only: the card list below md carries the
                        fields that decide where a key goes, and this one is
                        read on the node page when it matters. */}
                    <TableHead className="text-right">{t('nodes.column_telegram')}</TableHead>
                    <TableHead className="text-right">{t('nodes.column_heartbeat')}</TableHead>
                    <TableHead className="text-right">{t('nodes.column_changes')}</TableHead>
                    {isWriter && <TableHead className="w-0" />}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {nodes.map((node) => (
                    <NodeTableRow key={node.id} node={node} actions={rowActions(node)} />
                  ))}
                </TableBody>
              </Table>
            </div>

            <ul className="divide-y divide-hairline md:hidden">
              {nodes.map((node) => (
                <NodeCard key={node.id} node={node} actions={rowActions(node)} />
              ))}
            </ul>
          </Panel>
        )}
      </div>

      <CreateNodeDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={handleCreated} />

      {installResult && (
        <InstallCommandDialog
          open={!!installResult}
          onOpenChange={(open) => !open && setInstallResult(null)}
          nodeId={installResult.nodeId}
          command={installResult.command}
          expiresAt={installResult.expires_at}
        />
      )}

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('nodes.delete_confirm_title', { name: deleteTarget?.name ?? '' })}
        description={t('nodes.delete_confirm_description')}
        destructive
        confirmLabel={t('nodes.action_delete')}
        onConfirm={handleDelete}
      />
    </>
  );
}
