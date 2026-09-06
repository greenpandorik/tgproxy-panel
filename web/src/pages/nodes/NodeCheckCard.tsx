import { RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { useRunNodeCheck } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';
import { formatCompactAge, formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import type { Node, NodeCheckResult } from '@/api/types';

/**
 * Prerequisite checks in display order, matching internal/nodecheck.Checker.RunTelemt.
 * `pq_kex` is advisory (never counted in all_ok); `mask` is telemt-only and absent from a
 * tproxy node's report.
 */
const CHECK_NAMES = ['dns_a', 'tcp_80', 'tcp_443', 'tls_cert', 'pq_kex', 'http_root', 'mask'] as const;

/**
 * One prerequisite, as a row rather than a card: dot, what was checked, and
 * whatever the checker had to say about it in mono on the right. Read down the
 * column of dots and the failing step is the one that stops you. An advisory
 * probe that did not pass is a note, not a stop, so it takes the info tone
 * instead of red and carries its hint under the label.
 */
function CheckRow({ result }: { result: NodeCheckResult }) {
  const { t, i18n } = useTranslation();
  const tone = result.ok ? 'ok' : result.advisory ? 'info' : 'err';
  const hintKey = `nodes.check_${result.name}_hint`;
  const hint = i18n.exists(hintKey) ? t(hintKey) : null;
  return (
    <li className="flex flex-col gap-1 px-4 py-2.5 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
      <span className="flex min-w-0 items-start gap-2">
        <span
          className={cn(
            'mt-[6px] size-[7px] shrink-0 rounded-full',
            tone === 'ok' && 'bg-ok',
            tone === 'info' && 'bg-info',
            tone === 'err' && 'bg-err',
          )}
          aria-hidden="true"
        />
        <span className="flex min-w-0 flex-col">
          <span className="truncate text-sm text-foreground">{t(`nodes.check_${result.name}`, result.name)}</span>
          {hint && <span className="text-xs text-mute">{hint}</span>}
        </span>
      </span>
      {result.detail && (
        <span
          className={cn(
            'mono min-w-0 text-xs break-words sm:text-right',
            tone === 'ok' && 'text-dim',
            tone === 'info' && 'text-info',
            tone === 'err' && 'text-err',
          )}
        >
          {result.detail}
        </span>
      )}
    </li>
  );
}

export function NodeCheckCard({ node }: { node: Node }) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const runCheck = useRunNodeCheck(node.id);
  const report = node.last_check;

  const handleRun = async () => {
    try {
      await runCheck.mutateAsync();
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('nodes.check_request_failed'), type: 'error' });
    }
  };

  // While a run is in flight, prefer its live results over the stale
  // persisted report so the rows update the moment the response lands
  // (before the node query refetch confirms it).
  const results = runCheck.data?.results ?? report?.results;
  const allOk = runCheck.data?.all_ok ?? report?.all_ok;
  const ranAt = runCheck.data?.ran_at ?? report?.ran_at;
  const age = ranAt ? formatCompactAge(ranAt, i18n.language) : null;

  return (
    <Panel>
      <PanelHeader
        title={t('nodes.check_title')}
        meta={ranAt ? (age ? t('common.ago', { value: age }) : formatDateTime(ranAt, i18n.language)) : undefined}
        actions={
          <>
            {allOk !== undefined && (
              <span className={cn('mono text-xs', allOk ? 'text-ok' : 'text-err')}>
                {t(allOk ? 'nodes.check_status_ok' : 'nodes.check_status_failed')}
              </span>
            )}
            {isWriter && (
              <Button type="button" variant="outline" size="sm" onClick={() => void handleRun()} disabled={runCheck.isPending}>
                <RefreshCw className={cn(runCheck.isPending && 'animate-spin')} />
                {t('nodes.check_run')}
              </Button>
            )}
          </>
        }
      />

      {!results ? (
        <p className="px-4 py-6 text-center text-sm text-mute">{t('nodes.check_empty')}</p>
      ) : (
        <ul className="divide-y divide-hairline">
          {CHECK_NAMES.map((name) => {
            const result = results.find((r) => r.name === name);
            return result ? <CheckRow key={name} result={result} /> : null;
          })}
        </ul>
      )}
    </Panel>
  );
}
