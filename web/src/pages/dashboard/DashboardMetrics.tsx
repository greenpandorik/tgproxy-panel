import { ArrowDownUp, Radio, Server, Users } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { MetricValue } from '@/components/common/MetricValue';
import { isMetricPresent } from '@/components/common/metric';
import { Skeleton } from '@/components/ui/skeleton';
import { formatNumber, splitBytes } from '@/lib/format';
import { cn } from '@/lib/utils';

import type { Metric } from '@/components/common/metric';
import type { LucideIcon } from 'lucide-react';
import type { CSSProperties, ReactNode } from 'react';

export interface DashboardMetricsProps {
  nodesOnline: Metric<number>;
  nodesTotal: Metric<number>;
  keysActive: Metric<number>;
  sessions: Metric<number>;
  /** Octets moved over the last 24h, absent while no node has two samples to subtract. */
  traffic: Metric<number>;
  loading?: boolean;
}

interface Cell {
  id: string;
  icon: LucideIcon;
  label: string;
  to: string;
  tone: string;
  value: ReactNode;
}

const ABSENT_VALUE =
  '[&_[data-metric=absent]]:font-sans [&_[data-metric=absent]]:text-body [&_[data-metric=absent]]:font-normal [&_[data-metric=absent]]:tracking-normal';

/** The four numbers that give the verdict its context. Compact on purpose. */
export function DashboardMetrics({ nodesOnline, nodesTotal, keysActive, sessions, traffic, loading }: DashboardMetricsProps) {
  const { t, i18n } = useTranslation();
  const num = (value: number) => formatNumber(value, i18n.language);
  const bytes = isMetricPresent(traffic) ? splitBytes(traffic) : null;
  const someOffline = isMetricPresent(nodesOnline) && isMetricPresent(nodesTotal) && nodesOnline < nodesTotal;

  const cells: Cell[] = [
    {
      id: 'nodes',
      icon: Server,
      label: t('dashboard.nodes_online'),
      to: '/nodes',
      tone: someOffline ? 'var(--status-warn)' : 'var(--status-ok)',
      value: (
        <>
          <MetricValue value={nodesOnline} format={num} />
          <span className="text-mute" aria-hidden="true">
            {' / '}
          </span>
          <MetricValue value={nodesTotal} format={num} className="text-mute" />
        </>
      ),
    },
    {
      id: 'keys',
      icon: Users,
      label: t('dashboard.keys_active'),
      to: '/users',
      tone: 'var(--brand-primary)',
      value: <MetricValue value={keysActive} format={num} />,
    },
    {
      id: 'sessions',
      icon: Radio,
      label: t('dashboard.sessions_live'),
      to: '/monitoring',
      tone: 'var(--status-info)',
      value: <MetricValue value={sessions} format={num} />,
    },
    {
      id: 'traffic',
      icon: ArrowDownUp,
      label: t('dashboard.total_traffic'),
      to: '/monitoring',
      tone: 'var(--brand-accent)',
      value: <MetricValue value={bytes ? bytes.value : null} unit={bytes?.unit} />,
    },
  ];

  return (
    <div className="@container">
      <div className="grid grid-cols-2 gap-3 @min-[65rem]:grid-cols-4">
        {cells.map((cell) => (
          <Link
            key={cell.id}
            to={cell.to}
            className="flex items-center gap-4 rounded-surface border border-hairline-strong bg-card px-4 py-3 transition-[background-color,border-color] hover:border-brand-primary/40 hover:bg-elevated/40 sm:px-5 sm:py-4"
          >
            <span
              className="tgwp-tone-plate hidden size-11 shrink-0 items-center justify-center rounded-pill sm:flex"
              style={{ '--tone': cell.tone } as CSSProperties}
              aria-hidden="true"
            >
              <cell.icon size={20} strokeWidth={1.7} />
            </span>
            <span className="min-w-0">
              <span className="block text-label text-mute sm:truncate">{cell.label}</span>
              <span className={cn('mono mt-0.5 block text-[19px] leading-7 font-semibold tabular', ABSENT_VALUE)}>
                {loading ? <Skeleton className="h-6 w-16" /> : cell.value}
              </span>
            </span>
          </Link>
        ))}
      </div>
    </div>
  );
}
