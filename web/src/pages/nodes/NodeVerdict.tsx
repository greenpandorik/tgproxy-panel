import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { useAlerts } from '@/api/dashboard';
import { useNodeDiagnostics } from '@/api/web';
import { Panel } from '@/components/common/Panel';
import { TONE_VAR } from '@/components/common/statTone';
import { tallyChecks } from '@/components/web/diagnostics';
import { Button } from '@/components/ui/button';
import { formatCompactAge } from '@/lib/format';
import { alertTitle } from '@/pages/dashboard/alertTitle';

import type { Node } from '@/api/types';
import type { StatTone } from '@/components/common/statTone';

interface Verdict {
  tone: StatTone;
  headline: string;
  lines: string[];
}

/** The node's state in one sentence, with the list of what is wrong when something is. */
export function NodeVerdict({ node }: { node: Node }) {
  const { t, i18n } = useTranslation();
  const alertsQuery = useAlerts();
  const telemt = node.engine === 'telemt';
  const diagnostics = useNodeDiagnostics(node.id, 1);
  const alerts = (alertsQuery.data?.items ?? []).filter((a) => a.node_id === node.id);
  const lastRun = telemt ? diagnostics.data?.items?.[0] : undefined;
  const tally = tallyChecks(lastRun?.groups);
  const ago = (at: string | null | undefined) => {
    const age = at ? formatCompactAge(at, i18n.language) : null;
    return age ? t('common.ago', { value: age }) : null;
  };

  const verdict = ((): Verdict => {
    if (!node.online) {
      const seen = ago(node.last_seen_at);
      return {
        tone: 'err',
        headline: t('nodes.verdict_offline'),
        lines: [seen ? t('nodes.verdict_offline_seen', { ago: seen }) : t('nodes.verdict_offline_never')],
      };
    }
    if (alerts.length > 0) {
      return { tone: 'warn', headline: t('nodes.verdict_attention', { count: alerts.length }), lines: alerts.map((a) => alertTitle(a, t, i18n)) };
    }
    if (tally.failed + tally.warned > 0) {
      return { tone: 'warn', headline: t('nodes.verdict_diagnostics', { count: tally.failed + tally.warned }), lines: [t('nodes.verdict_diagnostics_hint')] };
    }
    if (node.status === 'degraded') {
      return { tone: 'warn', headline: t('nodes.verdict_degraded'), lines: [t('nodes.verdict_degraded_hint')] };
    }
    const checked = ago(lastRun?.finished_at ?? lastRun?.started_at);
    return {
      tone: 'ok',
      headline: t('nodes.verdict_ok'),
      lines: telemt ? [checked ? t('nodes.verdict_ok_checked', { ago: checked }) : t('nodes.verdict_ok_unchecked')] : [],
    };
  })();

  return (
    <div data-testid="node-verdict" data-tone={verdict.tone}>
      <Panel>
        <div className="flex flex-col gap-3 px-4 py-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="flex min-w-0 items-start gap-3">
            <span className="mt-[9px] size-2 shrink-0 rounded-pill" style={{ background: TONE_VAR[verdict.tone] }} aria-hidden="true" />
            <div className="min-w-0">
              <p className="text-title text-foreground">{verdict.headline}</p>
              {verdict.lines.length > 0 && (
                <ul className="mt-1 space-y-0.5 text-body text-mute">
                  {verdict.lines.map((line) => (
                    <li key={line}>{line}</li>
                  ))}
                </ul>
              )}
            </div>
          </div>
          {telemt && (
            <Button variant="outline" size="sm" className="shrink-0" nativeButton={false} render={<Link to="?section=diagnostics" />}>
              {t('nodes.verdict_open_diagnostics')}
            </Button>
          )}
        </div>
      </Panel>
    </div>
  );
}
