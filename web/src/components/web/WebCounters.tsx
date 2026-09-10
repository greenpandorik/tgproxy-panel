import { useTranslation } from 'react-i18next';

import { AdvancedSettings } from '@/components/common/AdvancedSettings';
import { MetricValue } from '@/components/common/MetricValue';
import { formatDateTime, formatNumber } from '@/lib/format';

import { WEB_COUNTERS } from './carrierDisplay';

import type { WebCarrierStats } from '@/api/types';

/**
 * The WEB counters telemt actually keeps, over the window. Each is nullable on purpose: a
 * family telemt never exported reads "not available" and not "0", which would claim health
 * the panel never measured.
 */
export function WebCounters({ stats }: { stats: WebCarrierStats }) {
  const { t, i18n } = useTranslation();
  const count = (v: number) => formatNumber(v, i18n.language);

  return (
    <div className="flex flex-col gap-4">
      <dl className="grid grid-cols-1 gap-px overflow-hidden rounded-control border border-hairline bg-hairline sm:grid-cols-2">
        {WEB_COUNTERS.map((counter) => (
          <div key={counter.id} data-testid={`web-counter-${counter.id}`} className="flex flex-col gap-0.5 bg-card px-4 py-3">
            <dt className="text-label text-mute">{t(counter.labelKey)}</dt>
            <dd className="text-title">
              <MetricValue value={stats[counter.id]} format={count} />
            </dd>
            <p className="text-micro text-mute">{t(counter.hintKey)}</p>
          </div>
        ))}
      </dl>

      <AdvancedSettings label={t('web.window_details')}>
        <dl className="grid grid-cols-1 gap-x-6 gap-y-2 text-label sm:grid-cols-2">
          <div className="flex items-baseline justify-between gap-3">
            <dt className="text-mute">{t('web.window_learning_entries')}</dt>
            <dd className="mono text-mono">
              <MetricValue value={stats.learning_entries} format={count} />
            </dd>
          </div>
          <div className="flex items-baseline justify-between gap-3">
            <dt className="text-mute">{t('web.window_samples')}</dt>
            <dd className="mono text-mono text-foreground">{count(stats.samples)}</dd>
          </div>
          <div className="flex items-baseline justify-between gap-3">
            <dt className="text-mute">{t('web.window_counter_resets')}</dt>
            <dd className="mono text-mono text-foreground">{count(stats.counter_resets)}</dd>
          </div>
          <div className="flex items-baseline justify-between gap-3">
            <dt className="text-mute">{t('web.window_range')}</dt>
            <dd className="mono text-mono text-mute">
              {formatDateTime(stats.from, i18n.language)} — {formatDateTime(stats.to, i18n.language)}
            </dd>
          </div>
        </dl>
        <p className="max-w-[72ch] text-label text-mute">{t('web.window_hint')}</p>
      </AdvancedSettings>
    </div>
  );
}
