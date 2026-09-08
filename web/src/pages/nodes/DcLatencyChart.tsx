// recharts is only ever imported by the chart components, which every page
// lazy-loads (React.lazy) so the shell bundle stays free of it.
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { useTranslation } from 'react-i18next';

import { ChartTooltip } from '@/components/common/ChartTooltip';
import { chartTextClass, formatTimeTick, gridProps, xAxisProps, yAxisProps } from '@/components/common/chartTheme';
import { ChartLegend } from '@/components/common/ChartLegend';

import { dcSeriesColor, dcSeriesKey } from './dcDisplay';

import type { DcLatencyRow } from './dcDisplay';

export interface DcLatencyChartProps {
  /** The DCs that appear in `rows`, in the order the legend lists them. */
  dcs: number[];
  rows: DcLatencyRow[];
  height?: number;
}

const DEFAULT_HEIGHT = 140;

/**
 * A node's latency to each Telegram datacenter over time, one line per DC
 * on one millisecond axis. They share a chart because they share a unit and
 * the question is comparative: which DC is the slow one, and did it get
 * slow at the same moment the others did (the node's uplink) or on its own
 * (that DC's route). The axis starts at zero so a quiet node's few-ms swing
 * is not blown up into a storm.
 */
export function DcLatencyChart({ dcs, rows, height = DEFAULT_HEIGHT }: DcLatencyChartProps) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const unit = t('common.ms');
  const formatMs = (v: number) => `${Math.round(v)} ${unit}`;

  const series = dcs.map((dc, i) => ({ key: dcSeriesKey(dc), name: `DC${dc}`, color: dcSeriesColor(i) }));

  return (
    <div>
      <div className={chartTextClass}>
        <ResponsiveContainer width="100%" height={height}>
          <LineChart data={rows} margin={{ top: 4, right: 24, bottom: 0, left: 0 }}>
            <CartesianGrid {...gridProps} />
            <XAxis {...xAxisProps} tickFormatter={(v: string) => formatTimeTick(v, locale)} />
            <YAxis {...yAxisProps} width={60} domain={[0, 'auto']} allowDecimals={false} tickFormatter={formatMs} />
            <Tooltip
              content={<ChartTooltip locale={locale} formatValue={formatMs} />}
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
      <ChartLegend items={series} className="mt-2" />
    </div>
  );
}
