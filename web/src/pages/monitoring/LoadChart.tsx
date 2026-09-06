// recharts is only ever imported by the chart components, which every page
// lazy-loads (React.lazy) so the shell bundle stays free of it.
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { useTranslation } from 'react-i18next';

import { ChartTooltip } from '@/components/common/ChartTooltip';
import { chartTextClass, formatTimeTick, gridProps, xAxisProps, yAxisProps } from '@/components/common/chartTheme';
import { ChartLegend } from '@/components/common/ChartLegend';

import type { LoadPoint } from '@/api/types';

export interface LoadChartProps {
  points: LoadPoint[];
  /** One colour per line: CPU, memory, disk. */
  colors: [string, string, string];
  height?: number;
}

const DEFAULT_HEIGHT = 140;

/** The load chart's y-axis ticks: a percentage axis is read against quarters, not against whatever recharts picks. */
const PERCENT_TICKS = [0, 25, 50, 75, 100];

/**
 * A node's server load over time: CPU, memory and disk as three lines on one
 * percentage axis. They share a chart, unlike the sessions and traffic
 * readings, because they share a unit - a reader can honestly compare "CPU at
 * 80" with "disk at 80", and the point of the chart is to see which resource
 * is the one under pressure. The axis is pinned to 0-100 so a quiet box does
 * not get its 3% swing blown up to look like a storm.
 */
export function LoadChart({ points, colors, height = DEFAULT_HEIGHT }: LoadChartProps) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const [cpu, mem, disk] = colors;

  const series = [
    { key: 'cpu_percent', name: t('nodes.load_cpu'), color: cpu },
    { key: 'mem_used_percent', name: t('nodes.load_ram'), color: mem },
    { key: 'disk_used_percent', name: t('nodes.load_disk'), color: disk },
  ];

  return (
    <div>
      <div className={chartTextClass}>
        <ResponsiveContainer width="100%" height={height}>
          <LineChart data={points} margin={{ top: 4, right: 24, bottom: 0, left: 0 }}>
            <CartesianGrid {...gridProps} />
            <XAxis {...xAxisProps} tickFormatter={(v: string) => formatTimeTick(v, locale)} />
            <YAxis {...yAxisProps} width={52} domain={[0, 100]} ticks={PERCENT_TICKS} tickFormatter={(v: number) => `${v}%`} />
            <Tooltip
              content={<ChartTooltip locale={locale} formatValue={(v) => `${Math.round(v)}%`} />}
              cursor={{ stroke: 'var(--line-2)', strokeWidth: 1 }}
              isAnimationActive={false}
            />
            {series.map((line) => (
              <Line
                key={line.key}
                type="monotone"
                dataKey={line.key}
                name={line.name}
                stroke={line.color}
                strokeWidth={1.6}
                dot={false}
                activeDot={{ r: 3, strokeWidth: 0 }}
                connectNulls
                isAnimationActive={false}
              />
            ))}
          </LineChart>
        </ResponsiveContainer>
      </div>
      <ChartLegend items={series} className="mt-1.5" />
    </div>
  );
}
