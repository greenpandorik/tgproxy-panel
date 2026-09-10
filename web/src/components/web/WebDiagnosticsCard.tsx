import { useState } from 'react';
import { Stethoscope } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { useNodeDiagnostics, useRunWebDiagnostics } from '@/api/web';
import { useAuth } from '@/auth/AuthProvider';
import { PanelEmpty } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { TONE_VAR } from '@/components/common/statTone';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ApiError } from '@/lib/api';
import { formatCompactAge, formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import { CHECK_TONE, tallyChecks } from './diagnostics';

import type { DiagnosticCheck, DiagnosticsRun } from '@/api/types';
import type { CSSProperties } from 'react';

function CheckRow({ check }: { check: DiagnosticCheck }) {
  const { t } = useTranslation();
  const tone = CHECK_TONE[check.status] ?? 'neutral';
  const skipped = check.status === 'not_available';

  return (
    <li
      data-check={check.key}
      data-status={check.status}
      className={cn(
        'flex flex-col gap-1 px-4 py-2.5 sm:flex-row sm:items-start sm:justify-between sm:gap-4',
        skipped && 'bg-elevated/40',
      )}
    >
      <span className="flex min-w-0 items-start gap-2">
        <span
          className="mt-[7px] size-[7px] shrink-0 rounded-pill"
          style={{ background: skipped ? 'transparent' : TONE_VAR[tone], boxShadow: skipped ? 'inset 0 0 0 1px var(--line-2)' : undefined }}
          aria-hidden="true"
        />
        <span className="flex min-w-0 flex-col">
          <span className={cn('truncate text-body', skipped ? 'text-mute' : 'text-foreground')}>
            {t(`web.check_${check.key}`, check.key)}
          </span>
          {check.detail && <span className="text-label text-mute">{check.detail}</span>}
          {(check.status === 'fail' || check.status === 'warn') && <span className="mt-1 text-label text-foreground">{t(`web.remedy_${check.key}`, t('web.remedy_default'))}</span>}
        </span>
      </span>
      <span className="flex shrink-0 items-center gap-2 sm:justify-end">
        {check.value && (
          <span className="mono text-mono break-words text-mute" style={{ color: skipped ? undefined : TONE_VAR[tone] }}>
            {check.value}
          </span>
        )}
        <span
          className={cn(
            'shrink-0 rounded-pill px-1.5 py-px text-micro',
            skipped ? 'border border-dashed border-hairline-strong text-mute' : 'tgwp-tone-tint border',
          )}
          style={skipped ? undefined : ({ '--tone': TONE_VAR[tone] } as CSSProperties)}
        >
          {t(`web.check_status_${check.status}`)}
        </span>
      </span>
    </li>
  );
}

function RunReport({ run }: { run: DiagnosticsRun }) {
  const { t } = useTranslation();
  const tally = tallyChecks(run.groups);

  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-hairline px-4 py-3 text-label">
        <span className="text-foreground">{t('web.diagnostics_passed', { passed: tally.passed, total: tally.total })}</span>
        {tally.failed > 0 && <span className="text-err">{t('web.diagnostics_failed', { count: tally.failed })}</span>}
        {tally.warned > 0 && <span className="text-warn">{t('web.diagnostics_warned', { count: tally.warned })}</span>}
        {tally.notRun > 0 && <span className="text-mute">{t('web.diagnostics_not_run', { count: tally.notRun })}</span>}
      </div>

      {run.groups.map((group) => (
        <section key={group.key} data-group={group.key}>
          <h3 className="border-b border-hairline bg-elevated/50 px-4 py-1.5 text-micro tracking-wide text-mute uppercase">
            {t(`web.group_${group.key}`, group.key)}
          </h3>
          <ul className="divide-y divide-hairline">
            {group.checks.map((check) => (
              <CheckRow key={check.key} check={check} />
            ))}
          </ul>
        </section>
      ))}

      <p className="px-4 py-3 text-label text-mute">{t('web.diagnostics_not_run_hint')}</p>
    </div>
  );
}

/** The WEB diagnostics pass: run it, and read it back exactly as the API grouped it. */
export function WebDiagnosticsCard({ nodeId }: { nodeId: string }) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const stored = useNodeDiagnostics(nodeId, 20);
  const [selected, setSelected] = useState<number | null>(null);
  const runDiagnostics = useRunWebDiagnostics(nodeId);

  const run = selected === null ? runDiagnostics.data ?? stored.data?.items?.[0] : stored.data?.items?.find((item) => item.id === selected);
  const ranAt = run?.finished_at ?? run?.started_at ?? null;
  const age = ranAt ? formatCompactAge(ranAt, i18n.language) : null;

  const runButton = isWriter && (
    <Button type="button" variant="outline" size="sm" onClick={() => { setSelected(null); runDiagnostics.mutate(); }} disabled={runDiagnostics.isPending}>
      <Stethoscope className={cn(runDiagnostics.isPending && 'animate-pulse')} />
      {t('web.diagnostics_run')}
    </Button>
  );

  return (
    <Panel>
      <PanelHeader
        icon={Stethoscope}
        title={t('web.diagnostics_title')}
        meta={ranAt ? (age ? t('common.ago', { value: age }) : formatDateTime(ranAt, i18n.language)) : undefined}
        actions={runButton}
      />

      {stored.data && stored.data.items.length > 1 && <div className="flex flex-wrap items-center gap-3 border-b border-hairline px-4 py-3"><label htmlFor={`diag-history-${nodeId}`} className="text-label text-mute">{t('web.diagnostics_history')}</label><select id={`diag-history-${nodeId}`} className="max-w-full rounded-control border border-hairline bg-background p-2 text-label" value={selected ?? ''} onChange={(e) => setSelected(e.target.value ? Number(e.target.value) : null)}><option value="">{t('web.diagnostics_latest')}</option>{stored.data.items.map((item) => <option key={item.id} value={item.id}>{formatDateTime(item.started_at, i18n.language)} · {t(`web.diagnostics_status_${item.overall_status}`, item.overall_status)}</option>)}</select></div>}
      {run && <div className="px-4 pt-3"><Button variant="ghost" size="sm" onClick={() => {
        const blob = new Blob([JSON.stringify(run, null, 2)], {type:'application/json'}); const url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href=url; link.download=`diagnostics-${nodeId}-${run.id}.json`; link.click(); window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      }}>{t('web.diagnostics_export')}</Button></div>}
      {runDiagnostics.isPending ? (
        <ul className="divide-y divide-hairline">
          {Array.from({ length: 6 }).map((_, i) => (
            <li key={i} className="flex items-center justify-between gap-4 px-4 py-3">
              <Skeleton className="h-3 w-44" />
              <Skeleton className="h-3 w-20 shrink-0" />
            </li>
          ))}
        </ul>
      ) : runDiagnostics.isError ? (
        <ErrorState
          inset
          message={runDiagnostics.error instanceof ApiError ? runDiagnostics.error.message : t('common.error_generic')}
          retryLabel={t('web.diagnostics_run')}
          onRetry={() => runDiagnostics.mutate()}
        />
      ) : stored.isError && !run ? <ErrorState inset message={stored.error.message} retryLabel={t('common.refresh')} onRetry={() => void stored.refetch()} /> : stored.isLoading ? (
        <div className="space-y-3 p-4">
          <Skeleton className="h-3 w-40" />
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-3/5" />
        </div>
      ) : run ? (
        <RunReport run={run} />
      ) : (
        <PanelEmpty>{isWriter ? t('web.diagnostics_empty') : t('web.diagnostics_empty_viewer')}</PanelEmpty>
      )}
    </Panel>
  );
}
