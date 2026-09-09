import { ArrowDownUp, KeyRound, Radio, Server } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { MetricValue } from '@/components/common/MetricValue';
import { isMetricPresent } from '@/components/common/metric';
import { Panel } from '@/components/common/Panel';
import { Skeleton } from '@/components/ui/skeleton';
import { formatNumber, splitBytes } from '@/lib/format';
import { cn } from '@/lib/utils';

import type { Metric } from '@/components/common/metric';
import type { LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';

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
  value: ReactNode;
}

function cellBorder(index: number): string {
  return cn(
    'border-hairline',
    index < 2 && 'border-b sm:border-b-0',
    index % 2 === 0 && 'border-r',
    index === 1 && 'sm:border-r',
  );
}

/** The four numbers that give the verdict its context. Compact on purpose. */
export function DashboardMetrics({ nodesOnline, nodesTotal, keysActive, sessions, traffic, loading }: DashboardMetricsProps) {
  const { t, i18n } = useTranslation();
  const num = (value: number) => formatNumber(value, i18n.language);
  const bytes = isMetricPresent(traffic) ? splitBytes(traffic) : null;

  const cells: Cell[] = [
    {
      id: 'nodes',
      icon: Server,
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
    },
    {
      id: 'keys',
      icon: KeyRound,
      label: t('dashboard.keys_active'),
      to: '/keys',
      value: <MetricValue value={keysActive} format={num} />,
    },
    {
      id: 'sessions',
      icon: Radio,
      label: t('dashboard.sessions_live'),
      to: '/monitoring',
      value: <MetricValue value={sessions} format={num} />,
    },
    {
      id: 'traffic',
      icon: ArrowDownUp,
      label: t('dashboard.total_traffic'),
      to: '/monitoring',
      value: <MetricValue value={bytes ? bytes.value : null} unit={bytes?.unit} />,
    },
  ];

  return (
    <Panel>
      <div className="grid grid-cols-2 sm:grid-cols-4">
        {cells.map((cell, index) => (
          <Link
            key={cell.id}
            to={cell.to}
            className={cn('block px-5 py-3.5 transition-colors hover:bg-elevated', cellBorder(index))}
          >
            <span className="flex min-h-8 items-start gap-2 text-label text-mute sm:min-h-0 sm:items-center">
              <cell.icon size={14} strokeWidth={1.8} className="mt-px shrink-0 sm:mt-0" aria-hidden="true" />
              <span className="line-clamp-2 min-w-0">{cell.label}</span>
            </span>
            <span className="mt-1.5 block text-title tabular">
              {loading ? <Skeleton className="h-5 w-16" /> : cell.value}
            </span>
          </Link>
        ))}
      </div>
    </Panel>
  );
}
