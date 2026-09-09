import { useTranslation } from 'react-i18next';

import { HEALTH_TONE } from '@/components/common/healthStatus';
import { MetricValue } from '@/components/common/MetricValue';
import { isMetricPresent } from '@/components/common/metric';
import { StatusIndicator } from '@/components/common/StatusIndicator';
import { TONE_VAR } from '@/components/common/statTone';
import { Skeleton } from '@/components/ui/skeleton';
import { formatNumber } from '@/lib/format';

import { fleetVerdict } from './verdict';

import type { FleetFacts } from './verdict';
import type { CSSProperties } from 'react';

const SHELL = 'flex flex-wrap items-center justify-between gap-x-5 gap-y-2 rounded-surface border px-5 py-4';

/** The page's answer in one sentence, with the count that backs it. */
export function VerdictLine({ facts, loading }: { facts: FleetFacts; loading?: boolean }) {
  const { t, i18n } = useTranslation();
  const num = (value: number) => formatNumber(value, i18n.language);

  if (loading) {
    return (
      <div className={`${SHELL} border-hairline-strong bg-card`} role="status" aria-busy="true">
        <span className="sr-only">{t('common.state.loading')}</span>
        <Skeleton className="h-5 w-64 max-w-full" />
        <Skeleton className="h-4 w-32" />
      </div>
    );
  }

  const verdict = fleetVerdict(facts);
  const problems = isMetricPresent(facts.attention) && facts.attention > 0 ? facts.attention : null;

  return (
    <section
      aria-live="polite"
      data-verdict={verdict.status}
      className={`tgwp-tone-tint ${SHELL}`}
      style={{ '--tone': TONE_VAR[HEALTH_TONE[verdict.status]] } as CSSProperties}
    >
      <StatusIndicator status={verdict.status} label={t(verdict.headline)} className="items-start text-title whitespace-normal sm:items-center" />

      <p className="mono flex flex-wrap items-center gap-x-1.5 text-mono text-mute">
        <span>{t('dashboard.verdict_fleet')}</span>
        <MetricValue value={facts.online} format={num} />
        <span aria-hidden="true">/</span>
        <MetricValue value={facts.total} format={num} />
        {problems !== null && (
          <>
            <span aria-hidden="true">·</span>
            <span>{t('dashboard.verdict_problems')}</span>
            <MetricValue value={problems} format={num} />
          </>
        )}
      </p>
    </section>
  );
}
