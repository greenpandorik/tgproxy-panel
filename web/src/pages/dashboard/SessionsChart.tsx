import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { useTranslation } from 'react-i18next';

import { ChartTooltip } from '@/components/common/ChartTooltip';
import { chartTextClass, formatTimeTick, gridProps, xAxisProps, yAxisProps } from '@/components/common/chartTheme';

export interface SessionsSeriesConfig {
  key: string;
  name: string;
  color: string;
}

export interface SessionsChartProps {
  data: Array<Record<string, number | string>>;
  series: SessionsSeriesConfig[];
}

// Live sessions per node over the last 24h: one thin line per node, no fill.
export function SessionsChart({ data, series }: SessionsChartProps) {
  const { i18n } = useTranslation();
  const locale = i18n.language;

  return (
    <div className={chartTextClass}>
      <ResponsiveContainer width="100%" height={220}>
        <LineChart data={data} margin={{ top: 8, right: 24, bottom: 0, left: 0 }}>
          <CartesianGrid {...gridProps} />
          <XAxis {...xAxisProps} tickFormatter={(v: string) => formatTimeTick(v, locale)} />
          <YAxis {...yAxisProps} allowDecimals={false} width={34} />
          <Tooltip
            content={<ChartTooltip locale={locale} />}
            cursor={{ stroke: 'var(--line-2)', strokeWidth: 1 }}
            isAnimationActive={false}
          />
          {series.map((s) => (
            <Line
              key={s.key}
              type="monotone"
              dataKey={s.key}
              name={s.name}
              stroke={s.color}
              strokeWidth={1.6}
              connectNulls
              dot={false}
              activeDot={{ r: 3, strokeWidth: 0 }}
              isAnimationActive={false}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
