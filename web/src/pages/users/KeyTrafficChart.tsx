import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { useTranslation } from 'react-i18next';

import { ChartTooltip } from '@/components/common/ChartTooltip';
import {
  bytesAxisFormatter,
  chartTextClass,
  formatTimeTick,
  gridProps,
  xAxisProps,
  yAxisProps,
} from '@/components/common/chartTheme';
import { formatBytes } from '@/lib/format';

export interface TrafficPoint {
  t: string;
  /** Octets moved since the previous sample. */
  bytes: number;
}

const CHART_HEIGHT = 110;

// One node's traffic for one key, sample by sample.
export function KeyTrafficChart({ points, color }: { points: TrafficPoint[]; color: string }) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const yTick = bytesAxisFormatter(Math.max(0, ...points.map((p) => p.bytes)));

  return (
    <div className={chartTextClass}>
      <ResponsiveContainer width="100%" height={CHART_HEIGHT}>
        <LineChart data={points} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid {...gridProps} />
          <XAxis {...xAxisProps} tickFormatter={(v: string) => formatTimeTick(v, locale)} />
          <YAxis {...yAxisProps} width={56} tickFormatter={yTick} />
          <Tooltip
            content={<ChartTooltip locale={locale} formatValue={(v) => formatBytes(v)} />}
            cursor={{ stroke: 'var(--line-2)', strokeWidth: 1 }}
            isAnimationActive={false}
          />
          <Line
            type="monotone"
            dataKey="bytes"
            name={t('keys.stats_traffic')}
            stroke={color}
            strokeWidth={1.6}
            dot={false}
            activeDot={{ r: 3, strokeWidth: 0 }}
            connectNulls
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
