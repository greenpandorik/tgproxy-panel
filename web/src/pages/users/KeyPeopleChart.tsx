import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { useTranslation } from 'react-i18next';

import { ChartTooltip } from '@/components/common/ChartTooltip';
import { chartTextClass, formatTimeTick, gridProps, xAxisProps, yAxisProps } from '@/components/common/chartTheme';
import { formatNumber } from '@/lib/format';

import type { KeyPresencePoint } from '@/api/types';

const CHART_HEIGHT = 140;

// People online on one shared user, bucket by bucket; an empty bucket is a gap.
export function KeyPeopleChart({ points, color }: { points: KeyPresencePoint[]; color: string }) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const num = (v: number) => formatNumber(v, locale);

  return (
    <div className={chartTextClass}>
      <ResponsiveContainer width="100%" height={CHART_HEIGHT}>
        <LineChart data={points} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid {...gridProps} />
          <XAxis {...xAxisProps} tickFormatter={(v: string) => formatTimeTick(v, locale)} />
          <YAxis {...yAxisProps} width={56} allowDecimals={false} domain={[0, 'auto']} tickFormatter={num} />
          <Tooltip
            content={
              <ChartTooltip
                locale={locale}
                formatValue={(v) => t('common.approx', { value: num(v), context: v === 0 ? 'zero' : undefined })}
              />
            }
            cursor={{ stroke: 'var(--line-2)', strokeWidth: 1 }}
            isAnimationActive={false}
          />
          <Line
            type="monotone"
            dataKey="people"
            name={t('common.people_online')}
            stroke={color}
            strokeWidth={1.6}
            dot={false}
            activeDot={{ r: 3, strokeWidth: 0 }}
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
