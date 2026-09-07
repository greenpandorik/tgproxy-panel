import {
  ChevronLeft,
  ChevronRight,
  Edit as EditIcon,
  ExternalLink,
  MoreHorizontal,
  Plus,
  RefreshCw,
  RotateCw,
  Trash2,
  Undo2,
} from 'lucide-react';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';

import { useBulkKeys, useDeleteKey, useKeys, useRevokeKey, useRotateKey } from '@/api/keys';
import { useNodes } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { DataTableSkeleton } from '@/components/common/DataTable';
import { EmptyState } from '@/components/common/EmptyState';
import { PageHeader } from '@/components/common/PageHeader';
import { Panel } from '@/components/common/Panel';
import { StatusBadge } from '@/components/common/StatusBadge';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import { Input } from '@/components/ui/input';
import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { toast } from '@/components/ui/toast';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { formatBytes, formatDate, formatDateTime, formatRelativeTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import { BatchResultDialog } from './BatchResultDialog';
import { BulkExtendDialog } from './BulkExtendDialog';
import { CreateKeyDialog } from './CreateKeyDialog';
import { KeyDetailDrawer } from './KeyDetailDrawer';
import { KeyLinkDialog } from './KeyLinkDialog';

import type { ReactNode } from 'react';
import type { AccessKey, BulkKeyAction, KeyStatus, KeyType } from '@/api/types';

const PER_PAGE = 50;

const TYPE_FILTER_LABEL: Record<KeyType | 'all', string> = {
  all: 'keys.filter_type_all',
  SHARED: 'keys.type_shared',
  PERSONAL: 'keys.type_personal',
};

const STATUS_FILTER_LABEL: Record<KeyStatus | 'all', string> = {
  all: 'keys.filter_status_all',
  pending: 'common.pending',
  active: 'common.active',
  revoked: 'common.revoked',
};

/** True once a key's expiry falls within 3 days (or has already passed) - the row's expiry cell turns red. */
function expiresSoon(iso: string | null): boolean {
  if (!iso) return false;
  const days = (new Date(iso).getTime() - Date.now()) / 86_400_000;
  return days < 3;
}

/** The host's own label, which is what distinguishes one node from another at a glance. */
function shortHost(hostname: string): string {
  return hostname.split('.')[0] || hostname;
}

interface RowActionsProps {
  keyRow: AccessKey;
  onShowLink: (id: string) => void;
  onEdit: (id: string) => void;
  onRotate: (key: AccessKey) => void;
  onRevoke: (key: AccessKey) => void;
  onDelete: (key: AccessKey) => void;
}

function RowActions({ keyRow, onShowLink, onEdit, onRotate, onRevoke, onDelete }: RowActionsProps) {
  const { t } = useTranslation();
  const revoked = keyRow.status === 'revoked';

  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="icon-sm" />}>
        <MoreHorizontal />
        <span className="sr-only">{t('common.actions')}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {!revoked && (
          <DropdownMenuItem onClick={() => onShowLink(keyRow.id)}>
            <ExternalLink />
            {t('keys.action_show_link')}
          </DropdownMenuItem>
        )}
        <DropdownMenuItem onClick={() => onEdit(keyRow.id)}>
          <EditIcon />
          {t('common.edit')}
        </DropdownMenuItem>
        {keyRow.type === 'SHARED' && !revoked && (
          <DropdownMenuItem onClick={() => onRotate(keyRow)}>
            <RotateCw />
            {t('keys.action_rotate')}
          </DropdownMenuItem>
        )}
        {!revoked && (
          <DropdownMenuItem variant="destructive" onClick={() => onRevoke(keyRow)}>
            <Undo2 />
            {t('keys.action_revoke')}
          </DropdownMenuItem>
        )}
        <DropdownMenuItem variant="destructive" onClick={() => onDelete(keyRow)}>
          <Trash2 />
          {t('common.delete')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** The key's kind, in the vocabulary the API and the profile files use. */
function TypeTag({ type }: { type: KeyType }) {
  return <Badge>{type}</Badge>;
}

function PendingStatus({ status }: { status: KeyStatus }) {
  const { t } = useTranslation();
  if (status !== 'pending') return <StatusBadge status={status} />;
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex" />}>
        <StatusBadge status={status} />
      </TooltipTrigger>
      <TooltipContent>{t('keys.status_pending_hint')}</TooltipContent>
    </Tooltip>
  );
}

function ExpiresCell({ iso, locale }: { iso: string | null; locale: string }) {
  const { t } = useTranslation();
  if (!iso) return <span className="text-label text-dim">{t('keys.no_expiry')}</span>;
  return (
    <span className={cn('text-label', expiresSoon(iso) ? 'text-err' : 'text-mute')} title={formatDateTime(iso, locale)}>
      {formatRelativeTime(iso, locale)}
    </span>
  );
}

function NodeChips({ nodes }: { nodes: AccessKey['nodes'] }) {
  if (nodes.length === 0) return <span className="mono text-mono text-dim">—</span>;
  return (
    <span className="flex flex-wrap gap-1">
      {nodes.map((n) => (
        <Badge key={n.node_id} title={n.hostname}>
          {shortHost(n.hostname)}
        </Badge>
      ))}
    </span>
  );
}

/**
 * The 30-day traffic of a key.
 *
 * Only telemt reports per-key traffic, so a key that sits only on tproxy nodes
 * has no figure at all - an em dash, not a zero, because "0 B" would read as "was
 * not used" when the truth is "was never measured". A key that is on a telemt
 * node and has moved nothing does get the zero.
 */
function TrafficCell({ bytes, measured }: { bytes: number; measured: boolean }) {
  if (bytes <= 0 && !measured) return <span className="mono text-mono text-dim">—</span>;
  return <span className={cn('mono text-mono', bytes > 0 ? 'text-mute' : 'text-dim')}>{formatBytes(bytes)}</span>;
}

/**
 * Label above value, for the narrow layout where there is no column header to
 * carry the name. The label is the same micro role the table head uses, because
 * it is the same thing: the column name, moved inside the row.
 */
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="micro truncate text-mute">{label}</dt>
      <dd className="mt-1 text-body">{children}</dd>
    </div>
  );
}

/**
 * Every key the operator has issued.
 *
 * The table is the page: a filter row on top, a bulk strip that appears only
 * once something is selected, and rows in which the label is the only thing
 * set in the reading face - hosts, dates and the key's kind are all machine
 * values and all mono, so a column of them can be compared without reading.
 * Red appears in exactly one place: an expiry inside three days.
 */
export function KeysPage() {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();

  const [searchInput, setSearchInput] = useState('');
  const [q, setQ] = useState('');
  const [type, setType] = useState<KeyType | 'all'>('all');
  const [status, setStatus] = useState<KeyStatus | 'all'>('all');
  const [node, setNode] = useState<string>('all');
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const [createRequested, setCreateRequested] = useState(false);
  const [linkKeyId, setLinkKeyId] = useState<string | null>(null);
  const [editRequestedId, setEditRequestedId] = useState<string | null>(null);
  const [batchResult, setBatchResult] = useState<AccessKey[] | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<AccessKey | null>(null);
  const [rotateTarget, setRotateTarget] = useState<AccessKey | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<AccessKey | null>(null);
  const [bulkRevokeOpen, setBulkRevokeOpen] = useState(false);
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false);
  const [bulkExtendOpen, setBulkExtendOpen] = useState(false);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setQ(searchInput.trim());
      setPage(1);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [searchInput]);

  // Deep links from the ⌘K palette: /keys?create=1 opens the create dialog and
  // /keys?key=<id> opens that key's drawer. Both are *derived* from the URL
  // rather than copied into state on mount, so there is no effect racing the
  // first render - closing the dialog is what drops the parameter.
  const [searchParams, setSearchParams] = useSearchParams();
  const dropParam = (name: string) => {
    if (!searchParams.has(name)) return;
    const next = new URLSearchParams(searchParams);
    next.delete(name);
    setSearchParams(next, { replace: true });
  };

  const createOpen = createRequested || searchParams.get('create') === '1';
  const setCreateOpen = (open: boolean) => {
    setCreateRequested(open);
    if (!open) dropParam('create');
  };

  const editKeyId = editRequestedId ?? searchParams.get('key');
  const setEditKeyId = (id: string | null) => {
    setEditRequestedId(id);
    if (id === null) dropParam('key');
  };

  const changeType = (v: KeyType | 'all') => {
    setType(v);
    setPage(1);
  };

  const changeStatus = (v: KeyStatus | 'all') => {
    setStatus(v);
    setPage(1);
  };

  const changeNode = (v: string) => {
    setNode(v);
    setPage(1);
  };

  const filters = {
    page,
    per_page: PER_PAGE,
    q: q || undefined,
    type: type === 'all' ? undefined : type,
    status: status === 'all' ? undefined : status,
    node: node === 'all' ? undefined : node,
  };

  const keysQuery = useKeys(filters);
  const nodesQuery = useNodes();
  const revokeKey = useRevokeKey();
  const rotateKey = useRotateKey();
  const deleteKey = useDeleteKey();
  const bulkKeys = useBulkKeys();

  const items = keysQuery.data?.items ?? [];
  const total = keysQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE));
  const nodes = nodesQuery.data?.items ?? [];
  // Which keys can have traffic at all: the key JSON carries the figure but not
  // the engines behind it, and the node list is already loaded for the filter.
  const telemtNodeIds = new Set(nodes.filter((n) => n.engine === 'telemt').map((n) => n.id));
  const measured = (key: AccessKey) => key.nodes.some((n) => telemtNodeIds.has(n.node_id));

  const toggleOne = (id: string, checked: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  const toggleAll = (checked: boolean) => {
    setSelected(checked ? new Set(items.map((k) => k.id)) : new Set());
  };

  const handleRevoke = async () => {
    if (!revokeTarget) return;
    try {
      await revokeKey.mutateAsync(revokeTarget.id);
      toast.add({ description: t('keys.revoke_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleRotate = async () => {
    if (!rotateTarget) return;
    try {
      await rotateKey.mutateAsync(rotateTarget.id);
      toast.add({ description: t('keys.rotate_success'), type: 'success' });
      setLinkKeyId(rotateTarget.id);
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await deleteKey.mutateAsync(deleteTarget.id);
      toast.add({ description: t('keys.delete_success'), type: 'success' });
      setSelected((prev) => {
        const next = new Set(prev);
        next.delete(deleteTarget.id);
        return next;
      });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const runBulk = async (action: BulkKeyAction, expiresAt?: string) => {
    try {
      const result = await bulkKeys.mutateAsync({ action, ids: [...selected], expires_at: expiresAt });
      const failedCount = Object.keys(result.failed).length;
      toast.add({
        description:
          failedCount > 0
            ? t('keys.bulk_result_partial', { done: result.done, failed: failedCount })
            : t('keys.bulk_result_success', { done: result.done }),
        type: failedCount > 0 ? 'warning' : 'success',
      });
      setSelected(new Set());
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const isLoading = keysQuery.isLoading;
  const columnCount = isWriter ? 9 : 7;

  const rowActions = (key: AccessKey) =>
    isWriter ? (
      <RowActions
        keyRow={key}
        onShowLink={setLinkKeyId}
        onEdit={setEditKeyId}
        onRotate={setRotateTarget}
        onRevoke={setRevokeTarget}
        onDelete={setDeleteTarget}
      />
    ) : null;

  return (
    <>
      <PageHeader
        title={t('keys.title')}
        description={total > 0 ? t('keys.header_total', { count: total }) : undefined}
        actions={
          <>
            <HelpButton topic="keys.list" />
            {isWriter && (
              <Button type="button" onClick={() => setCreateOpen(true)}>
                <Plus />
                {t('keys.create')}
              </Button>
            )}
          </>
        }
      />

      <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
        <Input
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
          placeholder={t('keys.search_placeholder')}
          className="sm:w-64"
          aria-label={t('common.search')}
        />
        <Select value={type} onValueChange={(v) => changeType(v as KeyType | 'all')}>
          <SelectTrigger className="w-full sm:w-36" aria-label={t('keys.column_type')}>
            {/* Resolve the label explicitly - SelectValue only reflects a matched item's
                rendered label once the popup has mounted at least once, so it would
                otherwise show the raw value ("all", "SHARED"). */}
            <SelectValue>{(v: KeyType | 'all') => t(TYPE_FILTER_LABEL[v] ?? TYPE_FILTER_LABEL.all)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t('keys.filter_type_all')}</SelectItem>
            <SelectItem value="SHARED">{t('keys.type_shared')}</SelectItem>
            <SelectItem value="PERSONAL">{t('keys.type_personal')}</SelectItem>
          </SelectContent>
        </Select>
        <Select value={status} onValueChange={(v) => changeStatus(v as KeyStatus | 'all')}>
          <SelectTrigger className="w-full sm:w-36" aria-label={t('keys.column_status')}>
            <SelectValue>{(v: KeyStatus | 'all') => t(STATUS_FILTER_LABEL[v] ?? STATUS_FILTER_LABEL.all)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t('keys.filter_status_all')}</SelectItem>
            <SelectItem value="pending">{t('common.pending')}</SelectItem>
            <SelectItem value="active">{t('common.active')}</SelectItem>
            <SelectItem value="revoked">{t('common.revoked')}</SelectItem>
          </SelectContent>
        </Select>
        <Select value={node} onValueChange={(v) => changeNode(v ?? 'all')}>
          <SelectTrigger className="w-full sm:w-44" aria-label={t('keys.column_nodes')}>
            {/* Without the render prop this shows a bare node UUID until the dropdown opens. */}
            <SelectValue>{(v: string) => nodes.find((n) => n.id === v)?.name ?? t('keys.filter_node_all')}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t('keys.filter_node_all')}</SelectItem>
            {nodes.map((n) => (
              <SelectItem key={n.id} value={n.id}>
                {n.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {isWriter && selected.size > 0 && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-surface border border-hairline-strong bg-card px-3 py-2">
          <span className="mono text-mono text-mute">{t('keys.bulk_selected', { count: selected.size })}</span>
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="outline" size="sm" onClick={() => setBulkExtendOpen(true)}>
              {t('keys.action_extend')}
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => setBulkRevokeOpen(true)}>
              {t('keys.action_revoke')}
            </Button>
            <Button type="button" variant="destructive" size="sm" onClick={() => setBulkDeleteOpen(true)}>
              {t('common.delete')}
            </Button>
          </div>
          <Button type="button" variant="ghost" size="sm" className="ml-auto" onClick={() => setSelected(new Set())}>
            {t('keys.bulk_clear')}
          </Button>
        </div>
      )}

      {isLoading ? (
        <DataTableSkeleton columns={columnCount} rows={6} />
      ) : keysQuery.isError ? (
        /* Without this branch a failed query falls through to the empty state
           and tells the operator they have no keys, which is a different and
           much worse thing than "the list did not load". */
        <EmptyState
          title={t('common.error_generic')}
          action={
            <Button type="button" variant="outline" disabled={keysQuery.isFetching} onClick={() => void keysQuery.refetch()}>
              <RefreshCw className={cn(keysQuery.isFetching && 'animate-spin')} />
              {t('common.refresh')}
            </Button>
          }
        />
      ) : items.length === 0 ? (
        <EmptyState
          title={t('keys.empty_title')}
          description={t('keys.empty_description')}
          action={
            isWriter && (
              <Button type="button" onClick={() => setCreateOpen(true)}>
                <Plus />
                {t('keys.create')}
              </Button>
            )
          }
        />
      ) : (
        <>
          <Panel className={ENTER_CLASS}>
            <div className="hidden md:block">
              <Table>
                <TableHeader>
                  <TableRow>
                    {isWriter && (
                      <TableHead className="w-0 pr-0 pl-4">
                        <Checkbox
                          checked={items.length > 0 && selected.size === items.length}
                          onCheckedChange={(v) => toggleAll(!!v)}
                          aria-label={t('keys.select_all')}
                        />
                      </TableHead>
                    )}
                    <TableHead>{t('keys.column_label')}</TableHead>
                    <TableHead>{t('keys.column_type')}</TableHead>
                    <TableHead>{t('keys.column_status')}</TableHead>
                    <TableHead>{t('keys.column_nodes')}</TableHead>
                    <TableHead className="text-right">{t('keys.column_traffic')}</TableHead>
                    <TableHead className="text-right">{t('keys.column_expires')}</TableHead>
                    <TableHead className="text-right">{t('keys.column_created')}</TableHead>
                    {isWriter && <TableHead className="w-0" />}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((key) => (
                    <TableRow key={key.id} data-state={selected.has(key.id) ? 'selected' : undefined}>
                      {isWriter && (
                        <TableCell className="w-0 pr-0 pl-4">
                          <Checkbox
                            checked={selected.has(key.id)}
                            onCheckedChange={(v) => toggleOne(key.id, !!v)}
                            aria-label={key.label}
                          />
                        </TableCell>
                      )}
                      <TableCell className="max-w-56">
                        <span className="block truncate text-body font-medium text-foreground">{key.label}</span>
                        {key.owner_label && <span className="block truncate text-label text-mute">{key.owner_label}</span>}
                      </TableCell>
                      <TableCell>
                        <TypeTag type={key.type} />
                      </TableCell>
                      <TableCell>
                        <PendingStatus status={key.status} />
                      </TableCell>
                      <TableCell className="max-w-56 whitespace-normal">
                        <NodeChips nodes={key.nodes} />
                      </TableCell>
                      <TableCell className="text-right">
                        <TrafficCell bytes={key.traffic_30d} measured={measured(key)} />
                      </TableCell>
                      <TableCell className="text-right">
                        <ExpiresCell iso={key.expires_at} locale={i18n.language} />
                      </TableCell>
                      <TableCell className="mono text-right text-mono text-dim">
                        {formatDate(key.created_at, i18n.language)}
                      </TableCell>
                      {isWriter && <TableCell className="w-0 text-right">{rowActions(key)}</TableCell>}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>

            <ul className="divide-y divide-hairline md:hidden">
              {items.map((key) => (
                <li key={key.id} className="px-4 py-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex min-w-0 items-start gap-3">
                      {isWriter && (
                        <Checkbox
                          className="mt-1"
                          checked={selected.has(key.id)}
                          onCheckedChange={(v) => toggleOne(key.id, !!v)}
                          aria-label={key.label}
                        />
                      )}
                      <div className="min-w-0">
                        <p className="truncate text-body font-medium text-foreground">{key.label}</p>
                        {key.owner_label && <p className="truncate text-label text-mute">{key.owner_label}</p>}
                      </div>
                    </div>
                    {rowActions(key)}
                  </div>

                  <dl className="mt-4 grid grid-cols-2 gap-x-4 gap-y-4">
                    <Field label={t('keys.column_type')}>
                      <TypeTag type={key.type} />
                    </Field>
                    <Field label={t('keys.column_status')}>
                      <PendingStatus status={key.status} />
                    </Field>
                    <Field label={t('keys.column_expires')}>
                      <ExpiresCell iso={key.expires_at} locale={i18n.language} />
                    </Field>
                    <Field label={t('keys.column_traffic')}>
                      <TrafficCell bytes={key.traffic_30d} measured={measured(key)} />
                    </Field>
                    <Field label={t('keys.column_created')}>
                      <span className="mono text-dim">{formatDate(key.created_at, i18n.language)}</span>
                    </Field>
                    <div className="col-span-2 min-w-0">
                      <dt className="micro truncate text-mute">{t('keys.column_nodes')}</dt>
                      <dd className="mt-1">
                        <NodeChips nodes={key.nodes} />
                      </dd>
                    </div>
                  </dl>
                </li>
              ))}
            </ul>
          </Panel>

          <div className={cn(ENTER_CLASS, 'flex items-center justify-between gap-2')} style={enterDelay(1)}>
            <p className="mono text-mono text-dim">{t('keys.pagination_summary', { page, totalPages, total })}</p>
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={page <= 1}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
              >
                <ChevronLeft />
                {t('keys.pagination_prev')}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={page >= totalPages}
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              >
                {t('keys.pagination_next')}
                <ChevronRight />
              </Button>
            </div>
          </div>
        </>
      )}

      <CreateKeyDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(created) => setLinkKeyId(created.id)}
        onBatchCreated={(keys) => setBatchResult(keys)}
      />

      <KeyLinkDialog open={!!linkKeyId} onOpenChange={(open) => !open && setLinkKeyId(null)} keyId={linkKeyId} />

      <KeyDetailDrawer open={!!editKeyId} onOpenChange={(open) => !open && setEditKeyId(null)} keyId={editKeyId} />

      {batchResult && (
        <BatchResultDialog
          open={!!batchResult}
          onOpenChange={(open) => !open && setBatchResult(null)}
          keys={batchResult}
          onShowLink={(id) => setLinkKeyId(id)}
        />
      )}

      <ConfirmDialog
        open={!!revokeTarget}
        onOpenChange={(open) => !open && setRevokeTarget(null)}
        title={t('keys.revoke_confirm_title', { label: revokeTarget?.label ?? '' })}
        description={revokeTarget?.type === 'SHARED' ? t('keys.revoke_confirm_shared') : t('keys.revoke_confirm_personal')}
        destructive
        confirmLabel={t('keys.action_revoke')}
        onConfirm={handleRevoke}
      />

      <ConfirmDialog
        open={!!rotateTarget}
        onOpenChange={(open) => !open && setRotateTarget(null)}
        title={t('keys.rotate_confirm_title', { label: rotateTarget?.label ?? '' })}
        description={t('keys.rotate_confirm_description')}
        confirmLabel={t('keys.action_rotate')}
        onConfirm={handleRotate}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('keys.delete_confirm_title', { label: deleteTarget?.label ?? '' })}
        description={t('keys.delete_confirm_description')}
        destructive
        confirmLabel={t('common.delete')}
        onConfirm={handleDelete}
      />

      <ConfirmDialog
        open={bulkRevokeOpen}
        onOpenChange={setBulkRevokeOpen}
        title={t('keys.bulk_revoke_title', { count: selected.size })}
        description={t('keys.bulk_revoke_description')}
        destructive
        confirmLabel={t('keys.action_revoke')}
        onConfirm={() => runBulk('revoke')}
      />

      <ConfirmDialog
        open={bulkDeleteOpen}
        onOpenChange={setBulkDeleteOpen}
        title={t('keys.bulk_delete_title', { count: selected.size })}
        description={t('keys.bulk_delete_description')}
        destructive
        confirmLabel={t('common.delete')}
        onConfirm={() => runBulk('delete')}
      />

      <BulkExtendDialog
        open={bulkExtendOpen}
        onOpenChange={setBulkExtendOpen}
        count={selected.size}
        onConfirm={(iso) => runBulk('extend', iso)}
      />
    </>
  );
}
