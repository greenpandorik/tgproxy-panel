import {
  CalendarPlus,
  Columns3,
  Copy,
  ListFilter,
  MoreHorizontal,
  Plus,
  Power,
  RefreshCw,
  Trash2,
  TriangleAlert,
  Users,
  X,
} from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
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
import { Pagination } from '@/components/common/Pagination';
import { Panel } from '@/components/common/Panel';
import { RowChevron } from '@/components/common/RowChevron';
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
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Input } from '@/components/ui/input';
import { ENTER_CLASS } from '@/components/ui/motion';
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

import type { AccessKey, BulkKeyAction, KeyState, KeySummary, KeyType } from '@/api/types';
import type { MouseEvent } from 'react';

const PAGE_SIZES = [25, 50, 100] as const;
const DEFAULT_PAGE_SIZE = 25;
const PAGE_SIZE_STORAGE = 'tgwp-users-page-size';

type StateFilter = KeyState | 'expiring' | 'all';

const STATE_FILTERS: StateFilter[] = ['all', 'active', 'pending', 'expiring', 'expired', 'disabled', 'revoked'];
const CHIPS: StateFilter[] = ['all', 'active', 'expiring', 'expired', 'disabled', 'pending', 'revoked'];
const RARE_CHIPS: StateFilter[] = ['pending', 'revoked'];
const TYPE_FILTERS: KeyType[] = ['PERSONAL', 'SHARED'];

const COLUMNS = ['online', 'expires', 'traffic', 'servers', 'type', 'link', 'created'] as const;
type Column = (typeof COLUMNS)[number];
const DEFAULT_COLUMNS: Column[] = ['online', 'expires', 'traffic', 'servers'];
const COLUMNS_STORAGE = 'tgwp-users-columns-v2';

const ROW_CONTROLS = 'a, button, input, label, [role="checkbox"], [role="menu"], [role="menuitem"], [data-row-ignore]';

function loadPageSize(): number {
  try {
    const saved = Number(localStorage.getItem(PAGE_SIZE_STORAGE));
    return (PAGE_SIZES as readonly number[]).includes(saved) ? saved : DEFAULT_PAGE_SIZE;
  } catch {
    return DEFAULT_PAGE_SIZE;
  }
}

function savePageSize(size: number): boolean {
  try {
    localStorage.setItem(PAGE_SIZE_STORAGE, String(size));
    return true;
  } catch {
    return false;
  }
}

function chipCount(id: StateFilter, s: KeySummary | undefined): number | undefined {
  if (!s) return undefined;
  if (id === 'all') return s.total;
  if (id === 'pending') return Math.max(0, s.total - s.active - s.expired - s.disabled - s.revoked);
  return s[id];
}

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

function StateChip({
  label,
  count,
  tone,
  pressed,
  title,
  onClick,
}: {
  label: string;
  count: number | undefined;
  tone?: string;
  pressed: boolean;
  title?: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      title={title}
      onClick={onClick}
      className={cn(
        'inline-flex h-8 shrink-0 items-center gap-2 rounded-control border px-3 text-label transition-[background-color,border-color,color,scale] outline-none active:scale-[0.985] focus-visible:ring-2 focus-visible:ring-ring',
        pressed
          ? 'border-primary/70 bg-primary/12 font-medium text-foreground'
          : 'border-hairline-strong text-mute hover:border-mute/55 hover:bg-elevated hover:text-foreground',
      )}
    >
      {label}
      {count !== undefined && <span className={cn('mono text-mono', pressed ? 'text-foreground' : (tone ?? 'text-mute'))}>{count}</span>}
    </button>
  );
}

function ActiveFilter({ label, removeLabel, onRemove }: { label: string; removeLabel: string; onRemove: () => void }) {
  return (
    <span className="inline-flex h-8 max-w-full items-center gap-1 rounded-control border border-primary/70 bg-primary/12 pr-1 pl-3 text-label text-foreground">
      <span className="truncate">{label}</span>
      <Button type="button" variant="ghost" size="icon-xs" onClick={onRemove} aria-label={removeLabel}>
        <X />
      </Button>
    </span>
  );
}

export function UsersPage() {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const [searchParams, setSearchParams] = useSearchParams();

  const q = searchParams.get('q') ?? '';
  const [searchInput, setSearchInput] = useState(q);
  const [syncedQ, setSyncedQ] = useState(q);
  if (q !== syncedQ) {
    setSyncedQ(q);
    setSearchInput(q);
  }
  const [pageSize, setPageSize] = useState(loadPageSize);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [columns, setColumns] = useState<Set<Column>>(loadColumns);
  const [welcomeId, setWelcomeId] = useState<string | null>(null);
  const [batchResult, setBatchResult] = useState<AccessKey[] | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<AccessKey | null>(null);
  const [bulk, setBulk] = useState<null | 'revoke' | 'delete' | 'extend'>(null);

  const stateParam = searchParams.get('state') as StateFilter | null;
  const state: StateFilter = stateParam && STATE_FILTERS.includes(stateParam) ? stateParam : 'all';
  const typeParam = searchParams.get('type') as KeyType | null;
  const type: KeyType | 'all' = typeParam && TYPE_FILTERS.includes(typeParam) ? typeParam : 'all';
  const node = searchParams.get('node');
  const page = Math.max(1, Number.parseInt(searchParams.get('page') ?? '', 10) || 1);
  const openId = searchParams.get('user') ?? searchParams.get('key');
  const createOpen = searchParams.get('create') === '1';

  // The selection follows the operator across pages of one result set, but a new search or filter
  // is a different set: users picked under the old one would be hidden from view yet still go to
  // the bulk action.
  const filterKey = JSON.stringify([q, state, type, node]);
  const [selectionFilterKey, setSelectionFilterKey] = useState(filterKey);
  if (filterKey !== selectionFilterKey) {
    setSelectionFilterKey(filterKey);
    setSelected(new Set());
  }

  const updateParams = (changes: Record<string, string | null>, opts: { push?: boolean; firstPage?: boolean } = {}) => {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        for (const [name, value] of Object.entries(changes)) {
          if (value === null) next.delete(name);
          else next.set(name, value);
        }
        if ('user' in changes) next.delete('key');
        if (opts.firstPage) next.delete('page');
        return next;
      },
      { replace: !opts.push },
    );
  };
  const setParam = (name: string, value: string | null) => updateParams({ [name]: value });
  const setFilter = (name: 'state' | 'type' | 'node', value: string | null) =>
    updateParams({ [name]: value }, { push: true, firstPage: true });
  const setPage = (next: number) => updateParams({ page: next > 1 ? String(next) : null }, { push: true });

  const setState = (v: StateFilter) => setFilter('state', v === 'all' ? null : v);
  const openUser = (id: string | null, welcome = false) => {
    setWelcomeId(welcome ? id : null);
    setParam('user', id);
  };

  const searchTimer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(searchTimer.current), []);
  const search = (value: string) => {
    setSearchInput(value);
    window.clearTimeout(searchTimer.current);
    searchTimer.current = window.setTimeout(() => {
      const next = value.trim();
      if (next === q) return;
      setSyncedQ(next);
      updateParams({ q: next || null }, { firstPage: true });
    }, 300);
  };

  const changePageSize = (size: number) => {
    const firstRow = (page - 1) * pageSize;
    setPageSize(size);
    savePageSize(size);
    const next = Math.floor(firstRow / size) + 1;
    updateParams({ page: next > 1 ? String(next) : null });
  };

  const filters = {
    page,
    per_page: pageSize,
    q: q || undefined,
    type: type === 'all' ? undefined : type,
    state: state === 'all' ? undefined : state,
    node: node ?? undefined,
  };
  const usersQuery = useKeys(filters);
  const summaryQuery = useKeySummary();
  const nodesQuery = useNodes();
  const bulkKeys = useBulkKeys();
  const deleteKey = useDeleteKey();
  const setDisabled = useSetKeyDisabled();

  const items = usersQuery.data?.items ?? [];
  const total = usersQuery.data?.total ?? 0;
  const lastPage = Math.max(1, Math.ceil(total / pageSize));
  const nodes = nodesQuery.data?.items ?? [];
  const telemtIds = new Set(nodes.filter((n) => n.engine === 'telemt').map((n) => n.id));
  const measured = (u: AccessKey) => u.nodes.some((n) => telemtIds.has(n.node_id));
  const summary = summaryQuery.data;
  const nodeName = node ? (nodes.find((n) => n.id === node)?.name ?? (nodesQuery.isLoading ? '…' : t('users.filter_server_unknown'))) : '';
  const typeName = type === 'SHARED' ? t('users.filter_type_shared') : t('users.filter_type_personal');

  const pageGone = !!usersQuery.data && !usersQuery.isPlaceholderData && items.length === 0 && total > 0 && page > 1;
  useEffect(() => {
    if (!pageGone) return;
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        if (lastPage > 1) next.set('page', String(lastPage));
        else next.delete('page');
        return next;
      },
      { replace: true },
    );
  }, [pageGone, lastPage, setSearchParams]);

  const chips = CHIPS.filter((id) => !RARE_CHIPS.includes(id) || state === id || (chipCount(id, summary) ?? 0) > 0);
  const chipTone = (id: StateFilter, count: number | undefined) =>
    !count ? undefined : id === 'expiring' ? 'text-warn' : id === 'expired' ? 'text-err' : undefined;

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
  const toggleAll = (checked: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev);
      for (const k of items) {
        if (checked) next.add(k.id);
        else next.delete(k.id);
      }
      return next;
    });
  const pageSelected = items.filter((k) => selected.has(k.id)).length;
  const onRowClick = (e: MouseEvent<HTMLElement>, id: string) => {
    if ((e.target as HTMLElement).closest(ROW_CONTROLS)) return;
    openUser(id);
  };

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
  const filtered = !!q || state !== 'all' || type !== 'all' || !!node;
  const extraFilters = (type !== 'all' ? 1 : 0) + (node ? 1 : 0);
  const noUsers = summary?.total === 0 && !filtered;
  const resetFilters = () => {
    window.clearTimeout(searchTimer.current);
    setSearchInput('');
    setSyncedQ('');
    updateParams({ q: null, state: null, type: null, node: null }, { push: true, firstPage: true });
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

      <div className={cn('flex flex-col gap-3', noUsers && 'hidden')}>
        <div className="flex flex-wrap items-center gap-2">
          <Input
            type="search"
            value={searchInput}
            onChange={(e) => search(e.target.value)}
            placeholder={t('users.search_placeholder')}
            className="w-full sm:max-w-md sm:flex-1"
            aria-label={t('common.search')}
          />
          <div className="ml-auto flex items-center gap-2">
            <DropdownMenu>
              <DropdownMenuTrigger render={<Button type="button" variant="outline" />}>
                <ListFilter />
                {t('users.filters')}
                {extraFilters > 0 && <span className="mono text-mono text-mute">{extraFilters}</span>}
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="min-w-56">
                <DropdownMenuGroup>
                  <DropdownMenuLabel>{t('users.column_type')}</DropdownMenuLabel>
                  <DropdownMenuRadioGroup value={type} onValueChange={(v) => setFilter('type', v === 'all' ? null : String(v))}>
                    <DropdownMenuRadioItem value="all" closeOnClick>
                      {t('keys.filter_type_all')}
                    </DropdownMenuRadioItem>
                    {TYPE_FILTERS.map((v) => (
                      <DropdownMenuRadioItem key={v} value={v} closeOnClick>
                        {t(v === 'SHARED' ? 'users.filter_type_shared' : 'users.filter_type_personal')}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuLabel>{t('users.filter_server')}</DropdownMenuLabel>
                  <DropdownMenuRadioGroup value={node ?? 'all'} onValueChange={(v) => setFilter('node', v === 'all' ? null : String(v))}>
                    <DropdownMenuRadioItem value="all" closeOnClick>
                      {t('keys.filter_node_all')}
                    </DropdownMenuRadioItem>
                    {nodes.map((n) => (
                      <DropdownMenuRadioItem key={n.id} value={n.id} closeOnClick>
                        {n.name}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuGroup>
              </DropdownMenuContent>
            </DropdownMenu>
            <DropdownMenu>
              <DropdownMenuTrigger render={<Button type="button" variant="outline" className="hidden md:inline-flex" />}>
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
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <div role="group" aria-label={t('users.filter_state')} className="contents">
            {chips.map((id) => {
              const count = chipCount(id, summary);
              return (
                <StateChip
                  key={id}
                  label={t(`users.filter_${id}`)}
                  count={count}
                  tone={chipTone(id, count)}
                  pressed={state === id}
                  title={id === 'expiring' ? t('users.filter_expiring_hint') : undefined}
                  onClick={() => state !== id && setState(id)}
                />
              );
            })}
          </div>
          {extraFilters > 0 && <span aria-hidden="true" className="mx-1 h-5 w-px bg-hairline-strong" />}
          {node && (
            <ActiveFilter
              label={t('users.filter_server_value', { name: nodeName })}
              removeLabel={t('users.filter_remove', { name: t('users.filter_server_value', { name: nodeName }) })}
              onRemove={() => setFilter('node', null)}
            />
          )}
          {type !== 'all' && (
            <ActiveFilter
              label={t('users.filter_type_value', { name: typeName })}
              removeLabel={t('users.filter_remove', { name: t('users.filter_type_value', { name: typeName }) })}
              onRemove={() => setFilter('type', null)}
            />
          )}
          {filtered && (
            <Button type="button" variant="ghost" size="sm" onClick={resetFilters}>
              <X />
              {t('users.reset_filters')}
            </Button>
          )}
        </div>
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
                          checked={items.length > 0 && pageSelected === items.length}
                          indeterminate={pageSelected > 0 && pageSelected < items.length}
                          onCheckedChange={(v) => toggleAll(!!v)}
                          aria-label={t('keys.select_all')}
                        />
                      </TableHead>
                    )}
                    <TableHead>{t('users.column_name')}</TableHead>
                    <TableHead>{t('keys.column_status')}</TableHead>
                    {show('online') && <TableHead>{t('users.column_online')}</TableHead>}
                    {show('expires') && <TableHead className="text-right">{t('users.column_expires')}</TableHead>}
                    {show('traffic') && <TableHead className="text-right">{t('users.column_traffic')}</TableHead>}
                    {show('servers') && <TableHead>{t('users.column_servers')}</TableHead>}
                    {show('type') && <TableHead>{t('users.column_type')}</TableHead>}
                    {show('link') && (
                      <TableHead className="w-0">
                        <span className="sr-only">{t('users.column_link')}</span>
                      </TableHead>
                    )}
                    {show('created') && <TableHead className="text-right">{t('users.column_created')}</TableHead>}
                    {isWriter && <TableHead className="w-0 px-0" />}
                    <TableHead className="w-0 pl-0" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((u) => (
                    <TableRow
                      key={u.id}
                      interactive
                      data-state={selected.has(u.id) ? 'selected' : undefined}
                      onClick={(e) => onRowClick(e, u.id)}
                    >
                      {isWriter && (
                        <TableCell className="w-0 pr-0" data-row-ignore="">
                          <Checkbox checked={selected.has(u.id)} onCheckedChange={(v) => toggleOne(u.id, !!v)} aria-label={u.label} />
                        </TableCell>
                      )}
                      <TableCell className="max-w-64">
                        <button
                          type="button"
                          className="block max-w-full rounded-control text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
                          onClick={() => openUser(u.id)}
                        >
                          <UserName user={u} />
                        </button>
                      </TableCell>
                      <TableCell>
                        <StateBadge state={u.state} />
                      </TableCell>
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
                      {show('traffic') && (
                        <TableCell className="mono text-right text-mono text-mute">
                          {measured(u) || u.traffic_30d > 0 ? formatBytes(u.traffic_30d) : '—'}
                        </TableCell>
                      )}
                      {show('servers') && (
                        <TableCell className="max-w-56 whitespace-normal">
                          <ServerChips nodes={u.nodes} />
                        </TableCell>
                      )}
                      {show('type') && (
                        <TableCell>
                          <Badge>{t(u.type === 'SHARED' ? 'keys.type_shared' : 'keys.type_personal')}</Badge>
                        </TableCell>
                      )}
                      {show('link') && (
                        <TableCell className="w-0" data-row-ignore="">
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
                      {show('created') && (
                        <TableCell className="mono text-right text-mono text-mute">{formatDate(u.created_at, i18n.language)}</TableCell>
                      )}
                      {isWriter && (
                        <TableCell className="w-0 px-0 text-right" data-row-ignore="">
                          {rowMenu(u)}
                        </TableCell>
                      )}
                      <TableCell className="w-0 pl-1">
                        <RowChevron />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>

            <ul className="divide-y divide-hairline md:hidden">
              {items.map((u) => (
                <li
                  key={u.id}
                  className="group/row flex cursor-pointer items-start gap-3 px-4 py-3 transition-colors hover:bg-elevated"
                  onClick={(e) => onRowClick(e, u.id)}
                >
                  {isWriter && (
                    <Checkbox
                      className="mt-1"
                      checked={selected.has(u.id)}
                      onCheckedChange={(v) => toggleOne(u.id, !!v)}
                      aria-label={u.label}
                    />
                  )}
                  <button
                    type="button"
                    className="min-w-0 flex-1 space-y-2 rounded-control text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    onClick={() => openUser(u.id)}
                  >
                    <UserName user={u} />
                    <span className="flex flex-wrap items-center gap-x-4 gap-y-1">
                      <StateBadge state={u.state} />
                      <ExpiresCell iso={u.expires_at} />
                      {u.live.online && <OnlineCell user={u} />}
                    </span>
                    <ServerChips nodes={u.nodes} />
                  </button>
                  {isWriter && rowMenu(u)}
                  <RowChevron className="mt-1" />
                </li>
              ))}
            </ul>
            <Pagination
              className="border-t border-hairline px-(--panel-x) py-3"
              page={page}
              pageSize={pageSize}
              total={total}
              onPageChange={setPage}
              pageSizes={PAGE_SIZES}
              onPageSizeChange={changePageSize}
            />
          </Panel>
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
            toggleOne(deleteTarget.id, false);
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
