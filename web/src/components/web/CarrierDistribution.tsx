import { useTranslation } from 'react-i18next';

import { MetricValue } from '@/components/common/MetricValue';
import { NotAvailableState } from '@/components/common/PageState';
import { formatNumber } from '@/lib/format';

import { CARRIER_I18N_KEY, carrierRows, distributionIsAbsent, drawableRows, formatShare } from './carrierDisplay';

import type { WebCarrierShare } from '@/api/types';

/**
 * How the WEB transport's carrier selections divide over the window, as shares.
 * This is a distribution of choices telemt recorded, not a count of anything live: telemt
 * exposes no per-carrier session gauge, so nothing here is labelled as sessions.
 */
export function CarrierDistribution({ distribution }: { distribution: WebCarrierShare[] | undefined }) {
  const { t, i18n } = useTranslation();
  const rows = carrierRows(distribution);

  if (distributionIsAbsent(rows)) {
    return (
      <NotAvailableState
        title={t('web.distribution_absent_title')}
        description={t('web.distribution_absent_description')}
        className="border-0 py-10"
      />
    );
  }

  const drawable = drawableRows(rows);

  return (
    <div className="flex flex-col gap-4" data-testid="carrier-distribution">
      <div
        className="flex h-2.5 w-full overflow-hidden rounded-pill bg-elevated"
        role="img"
        aria-label={drawable
          .map((row) => `${t(CARRIER_I18N_KEY[row.carrier])} ${formatShare(row.share, i18n.language)}`)
          .join(', ')}
      >
        {drawable.map((row) => (
          <span
            key={row.carrier}
            data-carrier={row.carrier}
            style={{ width: `${row.share * 100}%`, background: row.color }}
            className="h-full first:rounded-l-pill last:rounded-r-pill"
          />
        ))}
      </div>

      <ul className="flex flex-col gap-px">
        {rows.map((row) => (
          <li
            key={row.carrier}
            data-testid={`carrier-row-${row.carrier}`}
            className="flex items-center justify-between gap-3 py-1.5"
          >
            <span className="flex min-w-0 items-center gap-2">
              <span
                className="size-2.5 shrink-0 rounded-[3px]"
                style={{ background: row.share === null ? 'var(--line-2)' : row.color }}
                aria-hidden="true"
              />
              <span className="truncate text-body text-foreground">{t(CARRIER_I18N_KEY[row.carrier])}</span>
            </span>
            <span className="flex shrink-0 items-baseline gap-2">
              <MetricValue
                value={row.selections}
                format={(v) => formatNumber(v, i18n.language)}
                className="mono text-mono text-mute"
              />
              <span className="mono w-16 text-right text-mono">
                <MetricValue value={row.share} format={(v) => formatShare(v, i18n.language)} />
              </span>
            </span>
          </li>
        ))}
      </ul>

      <p className="text-label text-mute">{t('web.distribution_note')}</p>
    </div>
  );
}
