import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { MePoolReport } from '@/api/reliability';
import { SubSection } from '@/components/common/SubSection';
import { formatCompactDuration, formatNumber } from '@/lib/format';

/** Pool transitions are routine; unavailable or old observations cannot certify current health. */
export function NodeMePool({ snapshot }: { snapshot?: MePoolReport | null }) {
  const { t, i18n } = useTranslation();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 10_000);
    return () => window.clearInterval(timer);
  }, []);
  const age = snapshot ? (now - Date.parse(snapshot.at)) / 1000 : NaN;
  const available = snapshot?.enabled && snapshot.state;
  const fresh = Number.isFinite(age) && age <= 120 && age >= -30;
  if (!available || !fresh) {
    return (
      <SubSection title={t('me_pool.title')}>
        <p className="max-w-[72ch] text-label text-mute">{t(available ? 'me_pool.stale' : 'me_pool.unavailable')}</p>
      </SubSection>
    );
  }
  const state = available;
  const count = (value: number | null) => (value == null ? t('common.not_available') : formatNumber(value, i18n.language));
  const currentPending = state.pending && state.pending_map_current === true;
  const readings = [
    ['active_generation', count(state.active_generation)],
    [
      'pending_age',
      state.pending && state.pending_age_seconds != null
        ? formatCompactDuration(state.pending_age_seconds, i18n.language)
        : t('common.not_available'),
    ],
    ['pending_writers', count(currentPending ? state.pending_writers : null)],
    ['writer_deficit', count(currentPending ? state.writer_deficit : null)],
    ['missing_dc_groups', count(currentPending ? state.missing_dc_groups : null)],
    ['orphan_writers', count(state.orphan_warm_writers)],
    ['replacement_preparing', count(state.replacement_preparing)],
    ['replacement_retiring', count(state.replacement_retiring)],
  ];
  return (
    <SubSection title={t('me_pool.title')}>
      <p className="text-body">{t(state.pending ? 'me_pool.transition' : 'me_pool.no_transition')}</p>
      {state.pending && state.pending_map_current === false && <p className="text-label text-warn">{t('me_pool.stale_map')}</p>}
      <dl className="grid gap-x-6 sm:grid-cols-2 lg:grid-cols-3">
        {readings.map(([key, value]) => (
          <div key={key} className="flex flex-wrap justify-between gap-2 border-b border-hairline py-2.5">
            <dt className="text-label text-mute">{t(`me_pool.${key}`)}</dt>
            <dd className="text-mono tabular-nums">{value}</dd>
          </div>
        ))}
      </dl>
      <p className="max-w-[72ch] text-label text-mute">{t('me_pool.hint')}</p>
    </SubSection>
  );
}
