import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { MetricValue } from '@/components/common/MetricValue';
import { isMetricPresent } from '@/components/common/metric';
import { Sparkline } from '@/components/common/Sparkline';
import { Skeleton } from '@/components/ui/skeleton';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { formatNumber, splitBytes } from '@/lib/format';
import { cn } from '@/lib/utils';

import type { Metric } from '@/components/common/metric';
import type { ComponentProps, ReactNode } from 'react';

export interface DashboardMetricsProps {
  nodesOnline: Metric<number>;
  nodesTotal: Metric<number>;
  /** Names of the servers that are not answering, in the shared order. */
  nodesDown: string[];
  keysActive: Metric<number>;
  keysTotal: Metric<number>;
  /** People online across the fleet, an estimate the stats worker refreshes every minute. */
  people: Metric<number>;
  /** People seen at some point in the last 15 minutes. */
  people15m?: Metric<number>;
  /** People online over the last 24 hours, oldest first. */
  peopleSeries: number[];
  /** TCP connections on the servers that reported in the last few minutes. */
  connections: Metric<number>;
  /** Octets moved over the last 24h, absent while no server has two readings. */
  traffic: Metric<number>;
  /** Octets moved in the 24h before, absent unless the history covers that whole day. */
  trafficBefore: Metric<number>;
  loading?: boolean;
}

interface Tile {
  id: string;
  label: string;
  to: string;
  value: ReactNode;
  chart?: ReactNode;
  sub?: ReactNode;
  subTone?: string;
  hint?: ReactNode;
}

const ABSENT_VALUE =
  '[&_[data-metric=absent]]:font-sans [&_[data-metric=absent]]:text-body [&_[data-metric=absent]]:font-normal [&_[data-metric=absent]]:tracking-normal';

function TileLink({
  tile,
  loading,
  ...rest
}: { tile: Tile; loading?: boolean } & Omit<ComponentProps<typeof Link>, 'to' | 'className'>) {
  return (
    <Link
      {...rest}
      to={tile.to}
      className="flex min-w-0 flex-col gap-0.5 rounded-surface border border-hairline-strong bg-card px-4 py-3 transition-[background-color,border-color,scale] hover:border-brand-primary/40 hover:bg-elevated/40 focus-visible:outline-2 focus-visible:outline-ring active:scale-[0.985] sm:px-5 sm:py-4"
    >
      <span className="truncate text-label text-mute">{tile.label}</span>
      <span className={cn('mono block text-[19px] leading-7 font-semibold tabular', ABSENT_VALUE)}>
        {loading ? <Skeleton className="h-6 w-16" /> : tile.value}
      </span>
      {tile.chart && !loading && <span className="mt-1 block h-[22px]">{tile.chart}</span>}
      {!loading && (
        <span className={cn('mono mt-auto block truncate pt-1 text-micro', tile.subTone ?? 'text-mute')}>{tile.sub ?? ' '}</span>
      )}
    </Link>
  );
}

/** The change against the day before as a signed whole percent, or null when there is nothing to compare. */
function trafficDelta(traffic: Metric<number>, before: Metric<number>): number | null {
  if (!isMetricPresent(traffic) || !isMetricPresent(before) || before <= 0) return null;
  return Math.round(((traffic - before) / before) * 100);
}

/** The four numbers beside the inbox: people, servers, users, traffic. */
export function DashboardMetrics(props: DashboardMetricsProps) {
  const { nodesOnline, nodesTotal, nodesDown, keysActive, keysTotal, people, people15m, connections, traffic, loading } = props;
  const { t, i18n } = useTranslation();
  const num = (value: number) => formatNumber(value, i18n.language);
  const bytes = isMetricPresent(traffic) ? splitBytes(traffic, 1, i18n.language) : null;
  const delta = trafficDelta(traffic, props.trafficBefore);

  const downSub =
    nodesDown.length === 0
      ? isMetricPresent(nodesTotal) && nodesTotal > 0
        ? t('dashboard.tile_nodes_offline_none')
        : undefined
      : nodesDown.length === 1
        ? t('dashboard.tile_nodes_down_one', { name: nodesDown[0] })
        : t('dashboard.tile_nodes_down_many', { name: nodesDown[0], more: num(nodesDown.length - 1) });

  const tiles: Tile[] = [
    {
      id: 'people',
      label: t('common.people_online'),
      to: '/monitoring',
      value: <MetricValue value={people} format={(v) => t('common.approx', { value: num(v) })} />,
      chart:
        props.peopleSeries.length > 1 ? (
          <Sparkline points={props.peopleSeries} color="var(--brand-primary)" className="h-[22px] w-full" />
        ) : undefined,
      sub: isMetricPresent(connections) ? t('common.connections_count', { value: num(connections) }) : undefined,
      hint: (
        <span className="block max-w-xs space-y-1 py-0.5">
          {isMetricPresent(people15m) && <span className="block">{t('dashboard.people_15m', { value: num(people15m) })}</span>}
          <span className="block text-mute">{t('dashboard.people_hint')}</span>
        </span>
      ),
    },
    {
      id: 'nodes',
      label: t('dashboard.nodes_online'),
      to: '/nodes',
      value: (
        <>
          <MetricValue value={nodesOnline} format={num} />
          <span className="text-mute" aria-hidden="true">
            {' / '}
          </span>
          <MetricValue value={nodesTotal} format={num} className="text-mute" />
        </>
      ),
      sub: downSub,
      subTone: nodesDown.length > 0 ? 'text-err' : undefined,
    },
    {
      id: 'keys',
      label: t('dashboard.keys_active'),
      to: '/users',
      value: <MetricValue value={keysActive} format={num} />,
      sub: isMetricPresent(keysTotal) ? t('dashboard.tile_keys_of', { value: num(keysTotal) }) : undefined,
    },
    {
      id: 'traffic',
      label: t('dashboard.total_traffic'),
      to: '/monitoring',
      value: <MetricValue value={bytes ? bytes.value : null} unit={bytes?.unit} />,
      sub:
        delta === null
          ? undefined
          : t('dashboard.traffic_delta', { value: `${delta > 0 ? '+' : delta < 0 ? '−' : '±'}${num(Math.abs(delta))}%` }),
    },
  ];

  return (
    <div className="grid grid-cols-2 gap-3">
      {tiles.map((tile) =>
        tile.hint ? (
          <Tooltip key={tile.id}>
            <TooltipTrigger render={<TileLink tile={tile} loading={loading} />} />
            <TooltipContent side="bottom">{tile.hint}</TooltipContent>
          </Tooltip>
        ) : (
          <TileLink key={tile.id} tile={tile} loading={loading} />
        ),
      )}
    </div>
  );
}
