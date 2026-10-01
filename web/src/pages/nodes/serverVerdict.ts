import { formatCompactAge } from '@/lib/format';
import { alertTitle } from '@/pages/dashboard/alertTitle';

import type { CheckTally } from './nodeHealth';
import type { Alert, Node } from '@/api/types';
import type { TFunction, i18n as I18n } from 'i18next';

/** The server's state in one sentence, with what is wrong when something is. */
export interface Verdict {
  tone: 'ok' | 'warn' | 'err';
  headline: string;
  lines: string[];
}

export function nodeVerdict(
  node: Pick<Node, 'id' | 'online' | 'status' | 'last_seen_at'>,
  alerts: Alert[],
  checks: CheckTally,
  t: TFunction,
  i18n: I18n,
): Verdict {
  if (!node.online) {
    const age = node.last_seen_at ? formatCompactAge(node.last_seen_at, i18n.language) : null;
    return {
      tone: 'err',
      headline: t('nodes.verdict_offline'),
      lines: [age ? t('nodes.verdict_offline_seen', { ago: t('common.ago', { value: age }) }) : t('nodes.verdict_offline_never')],
    };
  }
  const own = alerts.filter((a) => a.node_id === node.id);
  if (own.length > 0) {
    return {
      tone: 'warn',
      headline: t('nodes.verdict_attention', { count: own.length }),
      lines: own.map((a) => alertTitle(a, t, i18n)),
    };
  }
  const problems = checks.failed + checks.warned;
  if (problems > 0) {
    return {
      tone: 'warn',
      headline: t('nodes.verdict_diagnostics', { count: problems }),
      lines: [t('nodes.verdict_diagnostics_hint')],
    };
  }
  if (node.status === 'degraded') {
    return { tone: 'warn', headline: t('nodes.verdict_degraded'), lines: [t('nodes.verdict_degraded_hint')] };
  }
  return { tone: 'ok', headline: t('nodes.verdict_ok'), lines: [] };
}
