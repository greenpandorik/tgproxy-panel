import { RefreshCw } from 'lucide-react';
import { Suspense, lazy, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

import { useKeyPresence } from '@/api/keys';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { seriesPalette } from '@/lib/chart';
import { formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';
import { useBrandingIdentity } from '@/theme/ThemeProvider';

import { nodeShares } from './presence';

import type { KeyStatsRange } from '@/api/keys';

const KeyPeopleChart = lazy(() => import('./KeyPeopleChart').then((m) => ({ default: m.KeyPeopleChart })));

const DEFAULT_BRAND_PRIMARY = '#0b7285';
const DEFAULT_BRAND_ACCENT = '#099268';

interface KeyPeopleSectionProps {
  keyId: string;
  range: KeyStatsRange;
}

/** People online on a shared user: now, over 15 minutes, by server and over the range. */
export function KeyPeopleSection({ keyId, range }: KeyPeopleSectionProps) {
  const { t, i18n } = useTranslation();
  const query = useKeyPresence(keyId, range);
  const { branding } = useBrandingIdentity();
  const locale = i18n.language;
  const num = (v: number) => formatNumber(v, locale);
  const approx = (v: number) => t('common.approx', { value: num(v), context: v === 0 ? 'zero' : undefined });
  const percent = useMemo(() => new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 0 }), [locale]);
  const color = useMemo(
    () => seriesPalette(branding?.primary_color || DEFAULT_BRAND_PRIMARY, branding?.accent_color || DEFAULT_BRAND_ACCENT, 1)[0],
    [branding?.primary_color, branding?.accent_color],
  );

  const data = query.data;
  const nodes = data?.nodes ?? [];
  const shares = nodeShares(nodes);
  const points = data?.points ?? [];
  const measured = points.filter((p) => p.people !== null).length;

  return (
    <section
      className="space-y-4 rounded-surface border border-hairline bg-surface px-4 py-3.5"
      aria-labelledby="key-people-title"
    >
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
        <div className="min-w-0">
          <p id="key-people-title" className="text-label text-mute">
            {t('common.people_online')}
          </p>
          {query.isLoading ? (
            <Skeleton className="mt-1.5 h-8 w-24" />
          ) : query.isError ? (
            <p className="mt-1 flex items-center gap-2 text-label text-destructive">
              {t('common.error_generic')}
              <Button type="button" variant="outline" size="sm" disabled={query.isFetching} onClick={() => void query.refetch()}>
                <RefreshCw className={cn(query.isFetching && 'animate-spin')} />
                {t('common.refresh')}
              </Button>
            </p>
          ) : (
            <>
              <p className="mono text-display text-foreground" data-testid="key-people-now">
                {approx(data?.now.people ?? 0)}
              </p>
              <p className="text-label text-mute">{t('keys.people_15m', { value: approx(data?.now.people_15m ?? 0) })}</p>
              <p className="text-label text-mute">{t('common.connections_count', { value: num(data?.now.connections ?? 0) })}</p>
            </>
          )}
        </div>
        {nodes.length > 0 && (
          <ul className="w-full space-y-2 sm:w-64" aria-label={t('keys.people_by_server')}>
            {nodes.map((node, i) => {
              const share = shares[i];
              return (
                <li key={node.node_id} className="space-y-1">
                  <div className="flex items-baseline justify-between gap-3 text-label">
                    <span className="truncate text-foreground">{node.node_name}</span>
                    <span className="mono shrink-0 text-mono text-mute">
                      {approx(node.people)}
                      {share !== null && ` · ${percent.format(share)}`}
                    </span>
                  </div>
                  <div className="h-1 overflow-hidden rounded-pill bg-elevated" aria-hidden="true">
                    <div className="h-full rounded-pill" style={{ width: `${(share ?? 0) * 100}%`, background: color }} />
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <div className="space-y-2">
        <p className="text-label text-foreground">{t(`keys.people_chart_${range}`)}</p>
        {query.isLoading ? (
          <Skeleton className="h-[140px] w-full" />
        ) : measured < 2 ? (
          <p className="flex h-[140px] items-center justify-center rounded-control border border-dashed border-hairline px-4 text-center text-label text-mute">
            {t('keys.stats_chart_empty')}
          </p>
        ) : (
          <Suspense fallback={<Skeleton className="h-[140px] w-full" />}>
            <KeyPeopleChart points={points} color={color} />
          </Suspense>
        )}
      </div>

      <p className="text-micro text-mute">{t('keys.people_hint')}</p>
    </section>
  );
}
