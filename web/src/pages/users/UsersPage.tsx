import {
  CalendarPlus,
  CalendarX2,
  ChevronLeft,
  ChevronRight,
  Columns3,
  Copy,
  Hourglass,
  MoreHorizontal,
  PauseCircle,
  Plus,
  Power,
  RefreshCw,
  Trash2,
  TriangleAlert,
  UserCheck,
  Users,
  X,
} from 'lucide-react';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';

import { useBulkKeys, useDeleteKey, useKeySummary, useKeys, useSetKeyDisabled } from '@/api/keys';
import { useNodes } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { CopyButton } from '@/components/common/CopyButton';
import { DataTableSkeleton } from '@/components/common/DataTable';
import { EmptyState } from '@/components/common/EmptyState';
import { PageHeader } from '@/components/common/PageHeader';
import { Panel } from '@/components/common/Panel';
import { StatTile } from '@/components/common/StatTile';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Input } from '@/components/ui/input';
import { ENTER_CLASS, enter, enterDelay } from '@/components/ui/motion';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { formatBytes, formatDate, formatDateTime, formatRelativeTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import { BatchResultDialog } from './BatchResultDialog';
import { BulkExtendDialog } from './BulkExtendDialog';
import { CreateUserDialog } from './CreateUserDialog';
import { StateBadge } from './StateBadge';
import { TONE_TEXT, expiryTone, shortHost } from './userState';
import { UserDialog } from './UserDialog';
import { extendExpiry, toLocalInputValue } from './userForm';

import type { StatTone } from '@/components/common/statTone';
import type { AccessKey, BulkKeyAction, KeyState, KeyType } from '@/api/types';
import type { LucideIcon } from 'lucide-react';

const PER_PAGE = 50;

type StateFilter = KeyState | 'expiring' | 'all';

const STATE_FILTERS: StateFilter[] = ['all', 'active', 'pending', 'expiring', 'expired', 'disabled', 'revoked'];

const COLUMNS = ['link', 'type', 'servers', 'traffic', 'online', 'expires', 'created'] as const;
type Column = (typeof COLUMNS)[number];
const DEFAULT_COLUMNS: Column[] = ['link', 'servers', 'traffic', 'online', 'expires'];
const COLUMNS_STORAGE = 'tgwp-users-columns-v2';

function loadColumns(): Set<Column> {
  try {
    const raw = localStorage.getItem(COLUMNS_STORAGE);
    if (raw) {
      const parsed = JSON.parse(raw) as string[];
      return new Set(parsed.filter((c): c is Column => (COLUMNS as readonly string[]).includes(c)));
    }
  } catch {
    // Storage can be off; the defaults still work.
  }
  return new Set(DEFAULT_COLUMNS);
}

function saveColumns(cols: Set<Column>) {
  try {
    localStorage.setItem(COLUMNS_STORAGE, JSON.stringify([...cols]));
  } catch {
    // Nothing to do: the choice lasts until the page reloads.
  }
}

function errText(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback;
}

function ExpiresCell({ iso }: { iso: string | null }) {
  const { t, i18n } = useTranslation();
  if (!iso) return <span className="text-label text-mute">{t('keys.no_expiry')}</span>;
  return (
    <span className={cn('text-label', TONE_TEXT[expiryTone(iso)])} title={formatDateTime(iso, i18n.language)}>
      {formatRelativeTime(iso, i18n.language)}
    </span>
  );
}

function OnlineCell({ user }: { user: AccessKey }) {
  const { t, i18n } = useTranslation();
  if (user.live.online) {
    return (
      <span className="inline-flex flex-col" title={t('users.devices_15m', { count: user.live.devices_15m })}>
        <span className="inline-flex items-center gap-2 text-label text-ok">
          <span className="size-[7px] rounded-pill bg-online" aria-hidden="true" />
          {t('users.online_devices', { count: user.live.devices })}
        </span>
        <span className="pl-[15px] text-micro text-mute">{t('users.online_now', { count: user.live.connections })}</span>
      </span>
    );
  }
  if (!user.last_seen_at) return <span className="mono text-mono text-dim">—</span>;
  return (
    <span className="inline-flex flex-col" title={formatDateTime(user.last_seen_at, i18n.language)}>
      <span className="text-label text-mute">{t('common.offline')}</span>
      <span className="text-micro text-mute">{formatRelativeTime(user.last_seen_at, i18n.language)}</span>
    </span>
  );
}

function ServerChips({ nodes }: { nodes: AccessKey['nodes'] }) {
  if (nodes.length === 0) return <span className="mono text-mono text-dim">—</span>;
  const shown = nodes.length > 3 ? nodes.slice(0, 2) : nodes;
  const rest = nodes.slice(shown.length);
  return (
    <span className="flex flex-wrap gap-1">
      {shown.map((n) => (
        <Badge key={n.node_id} title={n.hostname}>
          {n.node_name || shortHost(n.hostname)}
        </Badge>
      ))}
      {rest.length > 0 && <Badge title={rest.map((n) => n.node_name || n.hostname).join(', ')}>+{rest.length}</Badge>}
    </span>
  );
}

function UserName({ user }: { user: AccessKey }) {
  const { t } = useTranslation();
  return (
    <span className="block min-w-0">
      <span className="flex min-w-0 items-center gap-2">
        <span className="text-body font-medium wrap-break-word text-foreground">{user.label}</span>
        {user.type === 'SHARED' && <Badge className="shrink-0">{t('keys.type_shared')}</Badge>}
      </span>
      {(user.owner_label || user.sub_slug) && (
        <span className="block truncate text-label text-mute">
          {user.owner_label}
          {user.owner_label && user.sub_slug ? ' · ' : ''}
          {user.sub_slug && <span className="mono">/s/{user.sub_slug}</span>}
        </span>
      )}
    </span>
  );
}

interface TileSpec {
  id: StateFilter;
  icon: LucideIcon;
  tone: StatTone;
  value: number | undefined;
}

export function UsersPage() {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const [searchParams, setSearchParams] = useSearchParams();

  const [searchInput, setSearchInput] = useState('');
  const [q, setQ] = useState('');
  const [type, setType] = useState<KeyType | 'all'>('all');
  const [node, setNode] = useState('all');
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [columns, setColumns] = useState<Set<Column>>(loadColumns);
  const [welcomeId, setWelcomeId] = useState<string | null>(null);
  const [batchResult, setBatchResult] = useState<AccessKey[] | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<AccessKey | null>(null);
  const [bulk, setBulk] = useState<null | 'revoke' | 'delete' | 'extend'>(null);

  const stateParam = searchParams.get('state') as StateFilter | null;
  const state: StateFilter = stateParam && STATE_FILTERS.includes(stateParam) ? stateParam : 'all';
  const openId = searchParams.get('user') ?? searchParams.get('key');
  const createOpen = searchParams.get('create') === '1';

  const setParam = (name: string, value: string | null) => {
    const next = new URLSearchParams(searchParams);
    if (value === null) next.delete(name);
    else next.set(name, value);
    if (name === 'user') next.delete('key');
    setSearchParams(next, { replace: true });
  };

  const setState = (v: StateFilter) => {
    setParam('state', v === 'all' ? null : v);
    setPage(1);
  };
  const openUser = (id: string | null, welcome = false) => {
    setWelcomeId(welcome ? id : null);
    setParam('user', id);
  };

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setQ(searchInput.trim());
      setPage(1);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [searchInput]);

  const filters = {
    page,
    per_page: PER_PAGE,
    q: q || undefined,
    type: type === 'all' ? undefined : type,
    state: state === 'all' ? undefined : state,
    node: node === 'all' ? undefined : node,
  };
  const usersQuery = useKeys(filters);
  const summaryQuery = useKeySummary();
  const nodesQuery = useNodes();
  const bulkKeys = useBulkKeys();
  const deleteKey = useDeleteKey();
  const setDisabled = useSetKeyDisabled();

  const items = usersQuery.data?.items ?? [];
  const total = usersQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE));
  const nodes = nodesQuery.data?.items ?? [];
  const telemtIds = new Set(nodes.filter((n) => n.engine === 'telemt').map((n) => n.id));
  const measured = (u: AccessKey) => u.nodes.some((n) => telemtIds.has(n.node_id));
  const summary = summaryQuery.data;

  const tiles: TileSpec[] = [
    { id: 'all', icon: Users, tone: 'neutral', value: summary?.total },
    { id: 'active', icon: UserCheck, tone: 'ok', value: summary?.active },
    { id: 'expiring', icon: Hourglass, tone: summary && summary.expiring > 0 ? 'warn' : 'neutral', value: summary?.expiring },
    { id: 'expired', icon: CalendarX2, tone: summary && summary.expired > 0 ? 'err' : 'neutral', value: summary?.expired },
    { id: 'disabled', icon: PauseCircle, tone: 'neutral', value: summary?.disabled },
  ];

  const toggleColumn = (col: Column, on: boolean) => {
    const next = new Set(columns);
    if (on) next.add(col);
    else next.delete(col);
    setColumns(next);
    saveColumns(next);
  };
  const show = (col: Column) => columns.has(col);

  const toggleOne = (id: string, checked: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });
  const toggleAll = (checked: boolean) => setSelected(checked ? new Set(items.map((k) => k.id)) : new Set());

  const runBulk = async (action: BulkKeyAction, expiresAt?: string) => {
    try {
      const result = await bulkKeys.mutateAsync({ action, ids: [...selected], expires_at: expiresAt });
      const failed = Object.keys(result.failed).length;
      toast.add({
        description:
          failed > 0
            ? t('keys.bulk_result_partial', { done: result.done, failed })
            : t('users.bulk_done', { count: result.done }),
        type: failed > 0 ? 'warning' : 'success',
      });
      setSelected(new Set());
    } catch (err) {
      toast.add({ description: errText(err, t('common.error_generic')), type: 'error' });
    }
  };

  const copyLink = async (u: AccessKey) => {
    const link = u.subscription_short_url ?? u.subscription_url;
    if (!link) return;
    try {
      await navigator.clipboard.writeText(link);
      toast.add({ description: t('users.link_copied'), type: 'success' });
    } catch {
      toast.add({ description: t('common.error_generic'), type: 'error' });
    }
  };

  const toggleDisabled = async (u: AccessKey) => {
    try {
      await setDisabled.mutateAsync({ id: u.id, disabled: u.state !== 'disabled' });
      toast.add({ description: u.state === 'disabled' ? t('users.enabled') : t('users.disabled'), type: 'success' });
    } catch (err) {
      toast.add({ description: errText(err, t('common.error_generic')), type: 'error' });
    }
  };

  const rowMenu = (u: AccessKey) => (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="icon-sm" onClick={(e) => e.stopPropagation()} />}>
        <MoreHorizontal />
        <span className="sr-only">{t('common.actions')}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
        {(u.subscription_short_url ?? u.subscription_url) && (
          <DropdownMenuItem onClick={() => void copyLink(u)}>
            <Copy />
            {t('keys.subscription_copy')}
          </DropdownMenuItem>
        )}
        {u.state !== 'revoked' && (
          <DropdownMenuItem onClick={() => void extendMonth(u)}>
            <CalendarPlus />
            {t('users.extend_month')}
          </DropdownMenuItem>
        )}
        {u.state !== 'revoked' && (
          <DropdownMenuItem onClick={() => void toggleDisabled(u)}>
            <Power />
            {u.state === 'disabled' ? t('users.enable') : t('users.disable')}
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={() => setDeleteTarget(u)}>
          <Trash2 />
          {t('common.delete')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );

  const columnCount = 3 + COLUMNS.filter(show).length + (isWriter ? 2 : 0);
  const filtered = !!q || state !== 'all' || type !== 'all' || node !== 'all';
  const noUsers = summary?.total === 0 && !filtered;
  const resetFilters = () => {
    setSearchInput('');
    setQ('');
    setType('all');
    setNode('all');
    setState('all');
  };
  const extendMonth = async (u: AccessKey) => {
    try {
      const next = extendExpiry(toLocalInputValue(u.expires_at), '1m');
      await bulkKeys.mutateAsync({ action: 'extend', ids: [u.id], expires_at: new Date(next).toISOString() });
      toast.add({ description: t('users.extended', { date: formatDateTime(next, i18n.language) }), type: 'success' });
    } catch (err) {
      toast.add({ description: errText(err, t('common.error_generic')), type: 'error' });
    }
  };

  return (
    <>
      <PageHeader
        title={t('users.title')}
        actions={
          <>
            <HelpButton topic="keys.list" />
            {isWriter && (
              <Button type="button" onClick={() => setParam('create', '1')}>
                <Plus />
                {t('users.create')}
              </Button>
            )}
          </>
        }
      />

      <div className={cn('grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-5', noUsers && 'hidden')}>
        {tiles.map((tile, i) => (
          <button
            key={tile.id}
            type="button"
            onClick={() => setState(state === tile.id ? 'all' : tile.id)}
            aria-pressed={state === tile.id}
            style={enter(i).style}
            className={cn(
              enter(i).className,
              'rounded-surface text-left outline-offset-2 transition-shadow',
              i === 0 && 'col-span-2 md:col-span-1',
              state === tile.id && tile.id !== 'all' && 'ring-2 ring-brand-primary/60',
            )}
          >
            <StatTile
              icon={tile.icon}
              tone={tile.tone}
              label={t(`users.tile_${tile.id}`)}
              context={tile.id === 'expiring' ? t('users.tile_expiring_context') : undefined}
              value={tile.value ?? 0}
              loading={summaryQuery.isLoading}
              className="pointer-events-none"
            />
          </button>
        ))}
      </div>

      <div className={cn('grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center', noUsers && 'hidden')}>
        <Input
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
          placeholder={t('users.search_placeholder')}
          className="col-span-2 sm:max-w-sm sm:min-w-56 sm:flex-1"
          aria-label={t('common.search')}
        />
        <Select value={state} onValueChange={(v) => setState((v ?? 'all') as StateFilter)}>
          <SelectTrigger className="col-span-2 w-full sm:w-44" aria-label={t('keys.column_status')}>
            <SelectValue>{(v: StateFilter) => t(`users.filter_${v ?? 'all'}`)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {STATE_FILTERS.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`users.filter_${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={type}
          onValueChange={(v) => {
            setType((v ?? 'all') as KeyType | 'all');
            setPage(1);
          }}
        >
          <SelectTrigger className="w-full sm:w-36" aria-label={t('keys.column_type')}>
            <SelectValue>
              {(v: KeyType | 'all') =>
                t(v === 'SHARED' ? 'keys.type_shared' : v === 'PERSONAL' ? 'keys.type_personal' : 'keys.filter_type_all')
              }
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t('keys.filter_type_all')}</SelectItem>
            <SelectItem value="PERSONAL">{t('keys.type_personal')}</SelectItem>
            <SelectItem value="SHARED">{t('keys.type_shared')}</SelectItem>
          </SelectContent>
        </Select>
        <Select
          value={node}
          onValueChange={(v) => {
            setNode(v ?? 'all');
            setPage(1);
          }}
        >
          <SelectTrigger className="w-full sm:w-44" aria-label={t('keys.column_nodes')}>
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
        {filtered && (
          <Button type="button" variant="ghost" className="col-span-2" onClick={resetFilters}>
            <X />
            {t('users.reset_filters')}
          </Button>
        )}
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button type="button" variant="outline" className="hidden md:ml-auto md:inline-flex" />}>
            <Columns3 />
            {t('users.columns')}
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuGroup>
              <DropdownMenuLabel>{t('users.columns_hint')}</DropdownMenuLabel>
              {COLUMNS.map((col) => (
                <DropdownMenuCheckboxItem key={col} checked={show(col)} onCheckedChange={(on) => toggleColumn(col, !!on)}>
                  {t(`users.column_${col}`)}
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {isWriter && selected.size > 0 && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-surface border border-hairline-strong bg-card px-3 py-2">
          <span className="mono text-mono text-mute">{t('keys.bulk_selected', { count: selected.size })}</span>
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="outline" size="sm" onClick={() => setBulk('extend')}>
              {t('keys.action_extend')}
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => void runBulk('disable')}>
              {t('users.disable')}
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => void runBulk('enable')}>
              {t('users.enable')}
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => setBulk('revoke')}>
              {t('users.revoke')}
            </Button>
            <Button type="button" variant="destructive" size="sm" onClick={() => setBulk('delete')}>
              {t('common.delete')}
            </Button>
          </div>
          <Button type="button" variant="ghost" size="sm" className="ml-auto" onClick={() => setSelected(new Set())}>
            {t('keys.bulk_clear')}
          </Button>
        </div>
      )}

      {usersQuery.isLoading ? (
        <DataTableSkeleton columns={columnCount} rows={6} />
      ) : usersQuery.isError ? (
        <EmptyState
          icon={TriangleAlert}
          title={t('common.error_generic')}
          action={
            <Button type="button" variant="outline" disabled={usersQuery.isFetching} onClick={() => void usersQuery.refetch()}>
              <RefreshCw className={cn(usersQuery.isFetching && 'animate-spin')} />
              {t('common.refresh')}
            </Button>
          }
        />
      ) : items.length === 0 && filtered ? (
        <EmptyState
          icon={Users}
          title={t('users.empty_filtered')}
          action={
            <Button type="button" variant="outline" onClick={resetFilters}>
              {t('users.reset_filters')}
            </Button>
          }
        />
      ) : items.length === 0 ? (
        <EmptyState
          icon={Users}
          title={t('users.empty_title')}
          description={t('users.empty_description')}
          action={
            isWriter && (
              <Button type="button" onClick={() => setParam('create', '1')}>
                <Plus />
                {t('users.create')}
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
                      <TableHead className="w-0 pr-0">
                        <Checkbox
                          checked={items.length > 0 && selected.size === items.length}
                          onCheckedChange={(v) => toggleAll(!!v)}
                          aria-label={t('keys.select_all')}
                        />
                      </TableHead>
                    )}
                    <TableHead>{t('users.column_name')}</TableHead>
                    <TableHead>{t('keys.column_status')}</TableHead>
                    {show('link') && (
                      <TableHead className="w-0">
                        <span className="sr-only">{t('users.column_link')}</span>
                      </TableHead>
                    )}
                    {show('type') && <TableHead>{t('users.column_type')}</TableHead>}
                    {show('servers') && <TableHead>{t('users.column_servers')}</TableHead>}
                    {show('traffic') && <TableHead className="text-right">{t('users.column_traffic')}</TableHead>}
                    {show('online') && <TableHead>{t('users.column_online')}</TableHead>}
                    {show('expires') && <TableHead className="text-right">{t('users.column_expires')}</TableHead>}
                    {show('created') && <TableHead className="text-right">{t('users.column_created')}</TableHead>}
                    {isWriter && <TableHead className="w-0 pl-0" />}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((u) => (
                    <TableRow
                      key={u.id}
                      data-state={selected.has(u.id) ? 'selected' : undefined}
                      className="cursor-pointer"
                      onClick={() => openUser(u.id)}
                    >
                      {isWriter && (
                        <TableCell className="w-0 pr-0" onClick={(e) => e.stopPropagation()}>
                          <Checkbox checked={selected.has(u.id)} onCheckedChange={(v) => toggleOne(u.id, !!v)} aria-label={u.label} />
                        </TableCell>
                      )}
                      <TableCell className="max-w-64">
                        <button
                          type="button"
                          className="block max-w-full text-left underline-offset-4 hover:underline focus-visible:underline"
                          onClick={(e) => {
                            e.stopPropagation();
                            openUser(u.id);
                          }}
                        >
                          <UserName user={u} />
                        </button>
                      </TableCell>
                      <TableCell>
                        <StateBadge state={u.state} />
                      </TableCell>
                      {show('link') && (
                        <TableCell className="w-0" onClick={(e) => e.stopPropagation()}>
                          {(u.subscription_short_url ?? u.subscription_url) ? (
                            <CopyButton
                              value={(u.subscription_short_url ?? u.subscription_url) as string}
                              label={t('keys.subscription_copy')}
                              className="size-8"
                            />
                          ) : (
                            <span className="mono text-mono text-dim">—</span>
                          )}
                        </TableCell>
                      )}
                      {show('type') && (
                        <TableCell>
                          <Badge>{t(u.type === 'SHARED' ? 'keys.type_shared' : 'keys.type_personal')}</Badge>
                        </TableCell>
                      )}
                      {show('servers') && (
                        <TableCell className="max-w-56 whitespace-normal">
                          <ServerChips nodes={u.nodes} />
                        </TableCell>
                      )}
                      {show('traffic') && (
                        <TableCell className="mono text-right text-mono text-mute">
                          {measured(u) || u.traffic_30d > 0 ? formatBytes(u.traffic_30d) : '—'}
                        </TableCell>
                      )}
                      {show('online') && (
                        <TableCell>
                          <OnlineCell user={u} />
                        </TableCell>
                      )}
                      {show('expires') && (
                        <TableCell className="text-right">
                          <ExpiresCell iso={u.expires_at} />
                        </TableCell>
                      )}
                      {show('created') && (
                        <TableCell className="mono text-right text-mono text-mute">{formatDate(u.created_at, i18n.language)}</TableCell>
                      )}
                      {isWriter && (
                        <TableCell className="w-0 pl-0 text-right" onClick={(e) => e.stopPropagation()}>
                          {rowMenu(u)}
                        </TableCell>
                      )}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>

            <ul className="divide-y divide-hairline md:hidden">
              {items.map((u) => (
                <li key={u.id} className="flex items-start gap-3 px-4 py-3">
                  {isWriter && (
                    <Checkbox
                      className="mt-1"
                      checked={selected.has(u.id)}
                      onCheckedChange={(v) => toggleOne(u.id, !!v)}
                      aria-label={u.label}
                    />
                  )}
                  <button type="button" className="min-w-0 flex-1 space-y-2 text-left" onClick={() => openUser(u.id)}>
                    <UserName user={u} />
                    <span className="flex flex-wrap items-center gap-x-4 gap-y-1">
                      <StateBadge state={u.state} />
                      <ExpiresCell iso={u.expires_at} />
                      {u.live.online && <OnlineCell user={u} />}
                    </span>
                    <ServerChips nodes={u.nodes} />
                  </button>
                  {isWriter && rowMenu(u)}
                </li>
              ))}
            </ul>
          </Panel>

          {(totalPages > 1 || filtered) && (
            <div className={cn(ENTER_CLASS, 'flex flex-wrap items-center justify-between gap-2')} style={enterDelay(1)}>
              <p className="mono text-mono text-mute">
                {totalPages > 1
                  ? t('users.pagination_summary', { page, totalPages, count: total })
                  : t('users.found', { count: total })}
              </p>
              {totalPages > 1 && (
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
              )}
            </div>
          )}
        </>
      )}

      <CreateUserDialog
        open={createOpen}
        onOpenChange={(o) => setParam('create', o ? '1' : null)}
        onCreated={(created) => {
          const next = new URLSearchParams(searchParams);
          next.delete('create');
          next.set('user', created.id);
          setWelcomeId(created.id);
          setSearchParams(next, { replace: true });
        }}
        onBatchCreated={setBatchResult}
      />

      <UserDialog open={!!openId} onOpenChange={(o) => !o && openUser(null)} keyId={openId} welcome={!!openId && welcomeId === openId} />

      {batchResult && (
        <BatchResultDialog
          open={!!batchResult}
          onOpenChange={(o) => !o && setBatchResult(null)}
          keys={batchResult}
          onOpen={(id) => openUser(id)}
        />
      )}

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(o) => !o && setDeleteTarget(null)}
        title={t('users.delete_title', { label: deleteTarget?.label ?? '' })}
        description={t('users.delete_description')}
        destructive
        confirmLabel={t('common.delete')}
        onConfirm={async () => {
          if (!deleteTarget) return;
          try {
            await deleteKey.mutateAsync(deleteTarget.id);
            toast.add({ description: t('users.deleted'), type: 'success' });
          } catch (err) {
            toast.add({ description: errText(err, t('common.error_generic')), type: 'error' });
          }
        }}
      />

      <ConfirmDialog
        open={bulk === 'revoke'}
        onOpenChange={(o) => !o && setBulk(null)}
        title={t('users.bulk_revoke_title', { count: selected.size })}
        description={t('users.bulk_revoke_description')}
        destructive
        confirmLabel={t('users.revoke')}
        onConfirm={() => runBulk('revoke')}
      />
      <ConfirmDialog
        open={bulk === 'delete'}
        onOpenChange={(o) => !o && setBulk(null)}
        title={t('users.bulk_delete_title', { count: selected.size })}
        description={t('keys.bulk_delete_description')}
        destructive
        confirmLabel={t('common.delete')}
        onConfirm={() => runBulk('delete')}
      />
      <BulkExtendDialog
        open={bulk === 'extend'}
        onOpenChange={(o) => !o && setBulk(null)}
        count={selected.size}
        onConfirm={(iso) => runBulk('extend', iso)}
      />
    </>
  );
}
