import { ChevronLeft, ChevronRight, Info } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useAudit } from '@/api/audit';
import { DataTableSkeleton } from '@/components/common/DataTable';
import { EmptyState } from '@/components/common/EmptyState';
import { PageHeader } from '@/components/common/PageHeader';
import { Panel } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { HelpButton } from '@/help';
import { formatDateTime } from '@/lib/format';

const PER_PAGE = 50;

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

/**
 * The meta column shows as much of the payload as fits on one line and hides
 * the rest behind a popover holding the raw JSON. An audit row is evidence:
 * the compact form is for scanning a page of them, the pre is for reading the
 * one that matters, and neither is a summary the panel invented.
 */
function MetaCell({ meta }: { meta: unknown }) {
  const { t } = useTranslation();
  const hasMeta = meta !== null && meta !== undefined && (typeof meta !== 'object' || Object.keys(meta as object).length > 0);

  return (
    <div className="flex max-w-72 items-center gap-1">
      <span className="mono min-w-0 flex-1 truncate text-xs text-dim">{metaCompact(meta)}</span>
      {hasMeta && (
        <Popover>
          <PopoverTrigger render={<Button type="button" variant="ghost" size="icon-xs" className="-mr-1 shrink-0" />}>
            <Info />
            <span className="sr-only">{t('audit.details')}</span>
          </PopoverTrigger>
          <PopoverContent align="end" className="w-auto max-w-sm">
            <pre className="mono max-h-72 overflow-auto text-xs break-words whitespace-pre-wrap text-foreground">
              {JSON.stringify(meta, null, 2)}
            </pre>
          </PopoverContent>
        </Popover>
      )}
    </div>
  );
}

function TargetCell({ type, id }: { type: string; id: string }) {
  if (!type && !id) return <span className="text-dim">—</span>;
  return (
    <div className="min-w-0">
      {type && <div className="text-xs text-mute">{type}</div>}
      {id && <div className="mono truncate text-xs text-dim">{id}</div>}
    </div>
  );
}

/** The action name, as the server wrote it: a mono tag, never translated. */
function ActionTag({ action }: { action: string }) {
  return <span className="mono rounded-sm border border-hairline-strong px-1.5 py-0.5 text-xs text-mute">{action}</span>;
}

export function AuditPage() {
  const { t, i18n } = useTranslation();

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
        description={total > 0 ? t('audit.header_count', { count: total }) : undefined}
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
            className="mono min-w-0 sm:w-40"
          />
          <span className="text-dim">{t('audit.filter_date_to')}</span>
          <Input
            type="date"
            value={to}
            onChange={(e) => changeTo(e.target.value)}
            aria-label={t('audit.filter_to')}
            className="mono min-w-0 sm:w-40"
          />
        </div>
      </div>

      {isLoading ? (
        <DataTableSkeleton columns={6} rows={8} />
      ) : items.length === 0 ? (
        <EmptyState title={t('audit.empty_title')} description={t('audit.empty_description')} />
      ) : (
        <>
          <Panel>
            <div className="hidden md:block">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="pl-4">{t('audit.column_time')}</TableHead>
                    <TableHead>{t('audit.column_user')}</TableHead>
                    <TableHead>{t('audit.column_action')}</TableHead>
                    <TableHead>{t('audit.column_target')}</TableHead>
                    <TableHead>{t('audit.column_ip')}</TableHead>
                    <TableHead className="pr-4">{t('audit.column_meta')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((entry) => (
                    <TableRow key={entry.id}>
                      <TableCell className="mono pl-4 text-xs text-mute">
                        {formatDateTime(entry.created_at, i18n.language)}
                      </TableCell>
                      <TableCell className="text-foreground">{entry.username || t('audit.system_user')}</TableCell>
                      <TableCell>
                        <ActionTag action={entry.action} />
                      </TableCell>
                      <TableCell>
                        <TargetCell type={entry.target_type} id={entry.target_id} />
                      </TableCell>
                      <TableCell className="mono text-xs text-dim">{entry.ip || '—'}</TableCell>
                      <TableCell className="pr-4">
                        <MetaCell meta={entry.meta} />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>

            <ul className="divide-y divide-hairline md:hidden">
              {items.map((entry) => (
                <li key={entry.id} className="px-4 py-3">
                  <div className="flex items-start justify-between gap-2">
                    <ActionTag action={entry.action} />
                    <span className="mono shrink-0 text-xs text-dim">{formatDateTime(entry.created_at, i18n.language)}</span>
                  </div>
                  <p className="mt-2 text-sm text-foreground">{entry.username || t('audit.system_user')}</p>
                  <div className="mt-1">
                    <TargetCell type={entry.target_type} id={entry.target_id} />
                  </div>
                  <div className="mt-2 flex items-center justify-between gap-2">
                    <span className="mono text-xs text-dim">{entry.ip || '—'}</span>
                  </div>
                  <div className="mt-1">
                    <MetaCell meta={entry.meta} />
                  </div>
                </li>
              ))}
            </ul>
          </Panel>

          <div className="flex items-center justify-between gap-2">
            <p className="mono text-xs text-dim">{t('audit.pagination_summary', { page, totalPages, total })}</p>
            <div className="flex items-center gap-2">
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
