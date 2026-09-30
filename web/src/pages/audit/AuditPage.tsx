import { ChevronLeft, ChevronRight, Info, ScrollText, X } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useAudit } from '@/api/audit';
import { useNodes } from '@/api/nodes';
import { DataTableSkeleton } from '@/components/common/DataTable';
import { EmptyState } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { PageHeader } from '@/components/common/PageHeader';
import { Panel } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

const PER_PAGE = 50;

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function EntryTime({ at }: { at: string }) {
  const { i18n } = useTranslation();
  const d = new Date(at);
  if (Number.isNaN(d.getTime()) || d.getFullYear() !== new Date().getFullYear()) {
    return <>{formatDateTime(at, i18n.language)}</>;
  }
  const short = new Intl.DateTimeFormat(i18n.language, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
  return (
    <time dateTime={at} title={formatDateTime(at, i18n.language)}>
      {short.format(d)}
    </time>
  );
}

/** Fixed action-prefix categories the panel writes audit entries under (see server actions). */
const ACTION_PREFIXES = ['auth.', 'admin.', 'node.', 'key.', 'site_template.', 'branding.', 'settings.', 'alert.'] as const;

const ACTION_FILTER_LABEL: Record<(typeof ACTION_PREFIXES)[number] | 'all', string> = {
  all: 'audit.filter_action_all',
  'auth.': 'audit.action_auth',
  'admin.': 'audit.action_admin',
  'node.': 'audit.action_node',
  'key.': 'audit.action_key',
  'site_template.': 'audit.action_site_template',
  'branding.': 'audit.action_branding',
  'settings.': 'audit.action_settings',
  'alert.': 'audit.action_alert',
};

/** Renders a JSON meta object as a short "k=v, k2=v2" preview for the table cell. */
function metaCompact(meta: unknown, max = 3): string {
  if (meta === null || meta === undefined) return '—';
  if (typeof meta !== 'object' || Array.isArray(meta)) return JSON.stringify(meta);
  const entries = Object.entries(meta as Record<string, unknown>);
  if (entries.length === 0) return '—';
  const parts = entries
    .slice(0, max)
    .map(([k, v]) => `${k}=${typeof v === 'object' && v !== null ? JSON.stringify(v) : String(v)}`);
  return entries.length > max ? `${parts.join(', ')}, …` : parts.join(', ');
}

function hasMeta(meta: unknown): boolean {
  return meta !== null && meta !== undefined && (typeof meta !== 'object' || Object.keys(meta as object).length > 0);
}

function MetaCell({ meta }: { meta: unknown }) {
  const { t } = useTranslation();

  return (
    <div className="flex max-w-96 items-center gap-1">
      <span className="mono min-w-0 truncate text-mono text-mute">{metaCompact(meta)}</span>
      {hasMeta(meta) && (
        <Popover>
          <PopoverTrigger render={<Button type="button" variant="ghost" size="icon-xs" className="-my-1 shrink-0" />}>
            <Info />
            <span className="sr-only">{t('audit.details')}</span>
          </PopoverTrigger>
          <PopoverContent align="end" className="w-auto max-w-sm">
            <pre className="mono max-h-72 overflow-auto text-mono break-words whitespace-pre-wrap text-foreground">
              {JSON.stringify(meta, null, 2)}
            </pre>
          </PopoverContent>
        </Popover>
      )}
    </div>
  );
}

function TargetCell({ type, id, names }: { type: string; id: string; names: ReadonlyMap<string, string> }) {
  const { t, i18n } = useTranslation();
  if (!type && !id) return <span className="text-dim">—</span>;
  const typeKey = `audit.target.${type}`;
  const name = names.get(id);
  return (
    <span className="flex max-w-56 min-w-0 items-baseline gap-1.5 whitespace-nowrap">
      {type && <span className="text-label text-mute">{i18n.exists(typeKey) ? t(typeKey) : type}</span>}
      {id &&
        (name ? (
          <span className="truncate text-body text-foreground" title={id}>
            {name}
          </span>
        ) : (
          <span className="mono truncate text-mono text-mute" title={id}>
            {UUID.test(id) ? id.slice(0, 8) : id}
          </span>
        ))}
    </span>
  );
}

function Actor({ username, ip }: { username?: string; ip: string }) {
  const { t } = useTranslation();
  return (
    <span className="flex items-baseline gap-2 whitespace-nowrap">
      <span className="text-label text-foreground">{username || t('audit.system_user')}</span>
      {ip && <span className="mono text-mono text-mute">{ip}</span>}
    </span>
  );
}

function actionKey(action: string): string {
  if (action.startsWith('key.bulk_')) return 'key_bulk';
  if (action.startsWith('node.web_') && action !== 'node.web_policy') return 'node_web';
  return action.replace(/\./g, '_');
}

function ActionName({ action }: { action: string }) {
  const { t, i18n } = useTranslation();
  const key = `audit.act.${actionKey(action)}`;
  return (
    <span className="text-body text-foreground" title={action}>
      {i18n.exists(key) ? t(key) : action}
    </span>
  );
}

export function AuditPage() {
  const { t } = useTranslation();
  const nodesQuery = useNodes();
  const names = useMemo(() => new Map((nodesQuery.data?.items ?? []).map((n) => [n.id, n.name])), [nodesQuery.data]);

  const [action, setAction] = useState<(typeof ACTION_PREFIXES)[number] | 'all'>('all');
  const [userInput, setUserInput] = useState('');
  const [user, setUser] = useState('');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [page, setPage] = useState(1);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setUser(userInput.trim());
      setPage(1);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [userInput]);

  const changeAction = (v: (typeof ACTION_PREFIXES)[number] | 'all') => {
    setAction(v);
    setPage(1);
  };

  const changeFrom = (v: string) => {
    setFrom(v);
    setPage(1);
  };

  const changeTo = (v: string) => {
    setTo(v);
    setPage(1);
  };

  const filtered = action !== 'all' || userInput.trim() !== '' || from !== '' || to !== '';

  const resetFilters = () => {
    setAction('all');
    setUserInput('');
    setUser('');
    setFrom('');
    setTo('');
    setPage(1);
  };

  const filters = {
    page,
    per_page: PER_PAGE,
    action: action === 'all' ? undefined : action,
    user: user || undefined,
    from: from ? new Date(`${from}T00:00:00`).toISOString() : undefined,
    to: to ? new Date(`${to}T23:59:59.999`).toISOString() : undefined,
  };

  const auditQuery = useAudit(filters);
  const items = auditQuery.data?.items ?? [];
  const total = auditQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE));
  const isLoading = auditQuery.isLoading;

  return (
    <>
      <PageHeader
        title={t('audit.title')}
        description={total > 0 || (filtered && auditQuery.data) ? t('audit.header_count', { count: total }) : undefined}
        actions={<HelpButton topic="audit" />}
      />

      <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
        <Select value={action} onValueChange={(v) => changeAction((v ?? 'all') as (typeof ACTION_PREFIXES)[number] | 'all')}>
          <SelectTrigger className="w-full sm:w-48" aria-label={t('audit.column_action')}>
            <SelectValue>
              {(v: (typeof ACTION_PREFIXES)[number] | 'all') => t(ACTION_FILTER_LABEL[v] ?? ACTION_FILTER_LABEL.all)}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t('audit.filter_action_all')}</SelectItem>
            {ACTION_PREFIXES.map((prefix) => (
              <SelectItem key={prefix} value={prefix}>
                {t(ACTION_FILTER_LABEL[prefix])}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          value={userInput}
          onChange={(e) => setUserInput(e.target.value)}
          placeholder={t('audit.filter_user_placeholder')}
          className="sm:w-48"
          aria-label={t('audit.filter_user_placeholder')}
        />
        <div className="flex items-center gap-2">
          <Input
            type="date"
            value={from}
            onChange={(e) => changeFrom(e.target.value)}
            aria-label={t('audit.filter_from')}
            className="mono min-w-0 text-mono sm:w-40"
          />
          <span className="text-label text-mute">{t('audit.filter_date_to')}</span>
          <Input
            type="date"
            value={to}
            onChange={(e) => changeTo(e.target.value)}
            aria-label={t('audit.filter_to')}
            className="mono min-w-0 text-mono sm:w-40"
          />
        </div>
        {filtered && (
          <Button type="button" variant="ghost" onClick={resetFilters} className="self-start sm:self-auto">
            <X />
            {t('audit.filters_reset')}
          </Button>
        )}
      </div>

      {isLoading ? (
        <DataTableSkeleton columns={5} rows={8} />
      ) : auditQuery.isError ? (
        /* A failed page of the log is not an empty one: say which it was, and
           offer the ask-again the filters above cannot do on their own. */
        <ErrorState
          message={auditQuery.error instanceof ApiError ? auditQuery.error.message : t('common.error_generic')}
          retryLabel={t('common.refresh')}
          onRetry={() => void auditQuery.refetch()}
        />
      ) : items.length === 0 ? (
        filtered ? (
          <EmptyState
            icon={ScrollText}
            title={t('audit.empty_filtered_title')}
            action={
              <Button type="button" variant="outline" onClick={resetFilters}>
                {t('audit.filters_reset')}
              </Button>
            }
          />
        ) : (
          <EmptyState icon={ScrollText} title={t('audit.empty_title')} description={t('audit.empty_description')} />
        )
      ) : (
        <>
          <Panel className={ENTER_CLASS}>
            <div className="hidden md:block">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="pl-4">{t('audit.column_time')}</TableHead>
                    <TableHead>{t('audit.column_user')}</TableHead>
                    <TableHead>{t('audit.column_action')}</TableHead>
                    <TableHead>{t('audit.column_target')}</TableHead>
                    <TableHead className="pr-4">{t('audit.column_meta')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((entry) => (
                    <TableRow key={entry.id}>
                      <TableCell className="mono pl-4 text-mono whitespace-nowrap text-mute">
                        <EntryTime at={entry.created_at} />
                      </TableCell>
                      <TableCell>
                        <Actor username={entry.username} ip={entry.ip} />
                      </TableCell>
                      <TableCell>
                        <ActionName action={entry.action} />
                      </TableCell>
                      <TableCell>
                        <TargetCell type={entry.target_type} id={entry.target_id} names={names} />
                      </TableCell>
                      <TableCell className="w-full max-w-0 pr-4">
                        <MetaCell meta={entry.meta} />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>

            <ul className="divide-y divide-hairline md:hidden">
              {items.map((entry) => (
                <li key={entry.id} className="space-y-1.5 px-4 py-3">
                  <div className="flex items-start justify-between gap-3">
                    <ActionName action={entry.action} />
                    <span className="mono shrink-0 text-mono text-mute">
                      <EntryTime at={entry.created_at} />
                    </span>
                  </div>
                  <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
                    <Actor username={entry.username} ip={entry.ip} />
                    <TargetCell type={entry.target_type} id={entry.target_id} names={names} />
                  </div>
                  {hasMeta(entry.meta) && <MetaCell meta={entry.meta} />}
                </li>
              ))}
            </ul>
          </Panel>

          <div className={cn(ENTER_CLASS, 'flex items-center justify-between gap-2')} style={enterDelay(1)}>
            <p className="mono text-mono text-mute">{t('audit.pagination_summary', { page, totalPages, total })}</p>
            <div className={cn('flex items-center gap-2', totalPages <= 1 && 'hidden')}>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={page <= 1}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
              >
                <ChevronLeft />
                {t('audit.pagination_prev')}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={page >= totalPages}
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              >
                {t('audit.pagination_next')}
                <ChevronRight />
              </Button>
            </div>
          </div>
        </>
      )}
    </>
  );
}
