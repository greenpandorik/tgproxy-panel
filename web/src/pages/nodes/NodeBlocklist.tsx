import { ShieldBan, X } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useBlocklist, useSaveBlocklist } from '@/api/blocklist';
import { useAuth } from '@/auth/AuthProvider';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { Textarea } from '@/components/ui/textarea';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';
import { formatBytes, formatDate, formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';

import { parseBlockLines } from './blocklistLines';

import type { NewLine } from './blocklistLines';
import type { Blocklist, BlocklistEntry, BlocklistEntryInput } from '@/api/blocklist';
import type { Node } from '@/api/types';

function asInput(entries: BlocklistEntry[]): BlocklistEntryInput[] {
  return entries.map((e) => ({ prefix: e.prefix, note: e.note || undefined }));
}

function Status({ data, online }: { data: Blocklist; online: boolean }) {
  const { t } = useTranslation();
  const problem = data.apply_error || data.node_error;
  if (data.supported === false) return <p className="text-label text-warn">{t('blocklist.status_old_agent')}</p>;
  if (problem) return <p className="text-label text-destructive">{t('blocklist.status_error', { error: problem })}</p>;
  if (!data.synced && data.revision === 0) {
    return (
      <p className="text-label text-warn">
        {t(online ? 'blocklist.status_leftover' : 'blocklist.status_leftover_waiting', { count: data.node_entries ?? 0 })}
      </p>
    );
  }
  if (!data.synced) {
    return <p className="text-label text-warn">{t(online ? 'blocklist.status_sending' : 'blocklist.status_waiting')}</p>;
  }
  if (data.revision === 0) return null;
  return <p className="text-label text-ok">{t('blocklist.status_active')}</p>;
}

export function NodeBlocklist({ node }: { node: Node }) {
  const { t } = useTranslation();
  const query = useBlocklist(node.id);
  if (query.isLoading) return <Skeleton className="h-64 w-full" />;
  if (query.isError || !query.data) {
    return <ErrorState message={query.error?.message ?? ''} onRetry={() => void query.refetch()} retryLabel={t('common.refresh')} />;
  }
  return <BlocklistPanel node={node} data={query.data} />;
}

function BlocklistPanel({ node, data }: { node: Node; data: Blocklist }) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const save = useSaveBlocklist(node.id);
  const [text, setText] = useState('');
  const [lineErrors, setLineErrors] = useState<Record<number, string>>({});
  const [listErrors, setListErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState('');
  const [filter, setFilter] = useState('');

  const fresh = useMemo(() => parseBlockLines(text), [text]);
  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return data.entries;
    return data.entries.filter((e) => e.prefix.includes(q) || e.note.toLowerCase().includes(q));
  }, [data.entries, filter]);

  const explain = (msg: string) => {
    if (msg === 'too wide') return t('blocklist.error_too_wide');
    if (msg === 'not a public address') return t('blocklist.error_special');
    if (msg === 'not an address or network') return t('blocklist.error_invalid');
    const same = /^same as line (\d+)$/.exec(msg);
    if (same) return t('blocklist.error_duplicate');
    const covered = /^already covered by (.+)$/.exec(msg);
    if (covered) return t('blocklist.error_covered', { prefix: covered[1] });
    const covers = /^covers (.+) on line \d+$/.exec(msg);
    if (covers) return t('blocklist.error_covers', { prefix: covers[1] });
    return msg;
  };

  const submit = async (entries: BlocklistEntryInput[], added: NewLine[]) => {
    setLineErrors({});
    setListErrors({});
    setFormError('');
    try {
      const result = await save.mutateAsync({ revision: data.revision, entries });
      if (result.apply_error) {
        toast.add({ description: t('blocklist.saved_not_applied'), type: 'warning' });
      } else {
        toast.add({ description: t(result.synced ? 'blocklist.saved' : 'blocklist.saved_waiting'), type: 'success' });
      }
      return true;
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setFormError(t('blocklist.conflict'));
        return false;
      }
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        const byLine: Record<number, string> = {};
        const byPrefix: Record<string, string> = {};
        for (const [field, msg] of Object.entries(err.fields)) {
          const m = /^entries\.(\d+)$/.exec(field);
          if (!m) {
            setFormError(msg.startsWith('at most') ? t('blocklist.error_too_many', { max: data.max_entries }) : msg);
            continue;
          }
          const i = Number(m[1]);
          if (i >= data.entries.length && added[i - data.entries.length]) byLine[added[i - data.entries.length].line] = explain(msg);
          else if (entries[i]) byPrefix[entries[i].prefix] = explain(msg);
        }
        setLineErrors(byLine);
        setListErrors(byPrefix);
        return false;
      }
      setFormError(err instanceof ApiError ? err.message : t('common.error_generic'));
      return false;
    }
  };

  const add = async () => {
    if (fresh.length === 0) return;
    const ok = await submit([...asInput(data.entries), ...fresh.map((l) => ({ prefix: l.prefix, note: l.note || undefined }))], fresh);
    if (ok) setText('');
  };

  const remove = (prefix: string) => void submit(asInput(data.entries.filter((e) => e.prefix !== prefix)), []);

  const totals =
    data.dropped_packets !== null && data.entries.length > 0
      ? t('blocklist.dropped_total', {
          packets: formatNumber(data.dropped_packets, i18n.language),
          bytes: formatBytes(data.dropped_bytes ?? 0),
        })
      : null;

  return (
    <Panel>
      <PanelHeader
        icon={ShieldBan}
        title={t('blocklist.title')}
        meta={<span className="text-label text-mute">{t('blocklist.count', { count: data.entries.length })}</span>}
      />
      <PanelBody className="space-y-5">
        {(data.revision > 0 || data.supported === false || !data.synced) && (
          <div className="space-y-1.5">
            <Status data={data} online={node.online} />
            {totals && <p className="text-label text-mute">{totals}</p>}
          </div>
        )}

        {isWriter && data.supported !== false && (
          <div className="max-w-2xl space-y-2">
            <Label htmlFor="blocklist-add">{t('blocklist.add_label')}</Label>
            <Textarea
              id="blocklist-add"
              rows={3}
              spellCheck={false}
              autoComplete="off"
              className="mono text-mono"
              placeholder={'203.0.113.7 scanner\n198.51.100.0/24\n2001:db8::/32'}
              value={text}
              onChange={(e) => {
                setText(e.target.value);
                setLineErrors({});
              }}
            />
            {Object.keys(lineErrors).length > 0 ? (
              <ul className="space-y-0.5 text-label text-destructive">
                {Object.entries(lineErrors).map(([line, msg]) => (
                  <li key={line}>{t('blocklist.line_error', { line, error: msg })}</li>
                ))}
              </ul>
            ) : (
              <p className="text-label text-mute">{t('blocklist.add_hint')}</p>
            )}
            {formError && <p className="text-label text-destructive">{formError}</p>}
            <Button type="button" size="sm" disabled={fresh.length === 0 || save.isPending} onClick={() => void add()}>
              {fresh.length > 1 ? t('blocklist.add_many', { count: fresh.length }) : t('blocklist.add')}
            </Button>
          </div>
        )}

        <p className="max-w-[72ch] text-label text-mute">{t('blocklist.how_it_works')}</p>

        {data.entries.length === 0 ? (
          <p className="rounded-control border border-dashed border-hairline-strong px-4 py-6 text-center text-body text-mute">
            {t('blocklist.empty')}
          </p>
        ) : (
          <div className="space-y-2">
            {data.entries.length > 10 && (
              <Input
                className="max-w-xs"
                placeholder={t('blocklist.filter')}
                aria-label={t('blocklist.filter')}
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
              />
            )}
            <ul className="divide-y divide-hairline rounded-control border border-hairline">
              {shown.map((e) => (
                <li key={e.prefix} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 px-3 py-2 sm:grid-cols-[14rem_minmax(0,1fr)_auto_auto]">
                  <span className="mono truncate text-mono max-sm:col-start-1 max-sm:row-start-1">{e.prefix}</span>
                  <span className={cn('min-w-0 truncate text-body max-sm:col-start-1 max-sm:row-start-2', !e.note && 'text-mute')}>
                    {e.note || t('blocklist.no_note')}
                    <span className="ml-2 text-label text-mute">{formatDate(e.added_at, i18n.language)}</span>
                    {listErrors[e.prefix] && <span className="block text-label text-destructive">{listErrors[e.prefix]}</span>}
                  </span>
                  <span className="mono text-right text-mono text-mute max-sm:col-start-2 max-sm:row-start-2" title={t('blocklist.dropped_hint')}>
                    {e.packets === null ? '—' : t('blocklist.packets', { count: e.packets, value: formatNumber(e.packets, i18n.language) })}
                  </span>
                  {isWriter ? (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      aria-label={t('blocklist.remove', { prefix: e.prefix })}
                      disabled={save.isPending}
                      onClick={() => remove(e.prefix)}
                      className="justify-self-end max-sm:col-start-2 max-sm:row-start-1"
                    >
                      <X />
                    </Button>
                  ) : (
                    <span />
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </PanelBody>
    </Panel>
  );
}
