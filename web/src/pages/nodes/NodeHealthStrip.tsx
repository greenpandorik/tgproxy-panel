import { ChevronRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { MetricValue } from '@/components/common/MetricValue';
import { isMetricPresent } from '@/components/common/metric';
import { StatStrip } from '@/components/common/StatStrip';
import { formatCompactAge, formatNumber } from '@/lib/format';

import { checkTone, dcStateTone, usageTone } from './nodeHealth';

import type { CheckTally, DcState } from './nodeHealth';
import type { Verdict } from './serverVerdict';
import type { StatStripItem } from '@/components/common/StatStrip';
import type { Node, NodeHealth } from '@/api/types';
import type { SectionTone } from '@/components/common/CollapsibleSection';

const problem = (tone: SectionTone) => (tone === 'warn' || tone === 'err' ? tone : undefined);

function Pending() {
  const { t } = useTranslation();
  return (
    <span className="inline-block h-5 w-16 animate-pulse rounded-sm bg-foreground/8 align-middle">
      <span className="sr-only">{t('common.state.loading')}</span>
    </span>
  );
}

const percent = (v: number) => `${Math.round(v)}%`;

function Figure({ value, format }: { value: number | null | undefined; format: (v: number) => string }) {
  return isMetricPresent(value) ? <>{format(value)}</> : <MetricValue value={null} compact />;
}

/** The server's key numbers in one row: status, people, load, the way to Telegram and the last checks. */
export function NodeHealthStrip({
  node,
  verdict,
  health,
  loading,
  dcs,
  checks,
}: {
  node: Node;
  verdict: Verdict;
  health?: NodeHealth;
  loading: boolean;
  dcs: DcState | null;
  checks: CheckTally;
}) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const ago = (at: string | null | undefined) => {
    const age = at ? formatCompactAge(at, lang) : null;
    return age ? t('common.ago', { value: age }) : null;
  };
  const seen = ago(node.last_seen_at);
  const telemt = node.engine === 'telemt';
  const cpu = health?.cpu_utilisation_percent;
  const mem = health?.mem_used_percent;

  const items: StatStripItem[] = [
    {
      id: 'status',
      label: t('nodes.detail_strip_status'),
      value: t(`nodes.detail_status_${verdict.tone}`),
      sub: seen
        ? t(node.online ? 'nodes.detail_status_seen' : 'nodes.detail_status_last_seen', { ago: seen })
        : t('nodes.last_seen_never'),
      tone: verdict.tone,
    },
    {
      id: 'people',
      label: t('common.people_online'),
      value: (
        <span className="inline-flex items-center gap-1">
          {isMetricPresent(node.people_online) ? (
            t('common.approx', { value: formatNumber(node.people_online, lang) })
          ) : (
            <MetricValue value={null} compact />
          )}
          <ChevronRight className="size-4 shrink-0 text-mute" aria-hidden="true" />
        </span>
      ),
      sub: isMetricPresent(node.connections)
        ? t('common.connections_count', { value: formatNumber(node.connections, lang) })
        : undefined,
      to: `/users?node=${node.id}`,
    },
    {
      id: 'load',
      label: t('nodes.detail_strip_load'),
      value:
        loading && !health ? (
          <Pending />
        ) : (
          <>
            <Figure value={cpu} format={percent} /> · <Figure value={mem} format={percent} />
          </>
        ),
      sub: t('nodes.detail_strip_load_sub'),
      tone: problem(usageTone(cpu, mem)),
    },
    ...(telemt
      ? [
          {
            id: 'telegram',
            label: t('nodes.detail_strip_telegram'),
            value:
              loading && !health ? (
                <Pending />
              ) : (
                <Figure value={dcs?.latency} format={(ms) => `${formatNumber(Math.round(ms), lang)} ${t('common.ms')}`} />
              ),
            sub: dcs ? t('nodes.detail_strip_dcs', { measured: dcs.measured, total: dcs.total }) : undefined,
            tone: dcs ? problem(dcStateTone(dcs)) : undefined,
          },
        ]
      : []),
    {
      id: 'checks',
      label: t('nodes.detail_strip_checks'),
      value:
        checks.total > 0 ? (
          t('nodes.detail_checks_value', { passed: checks.passed, total: checks.total })
        ) : (
          <MetricValue value={null} compact />
        ),
      sub: ago(checks.ranAt) ?? t('nodes.detail_checks_never'),
      tone: problem(checkTone(checks)),
    },
  ];

  return <StatStrip label={t('nodes.detail_strip_label')} items={items} />;
}
