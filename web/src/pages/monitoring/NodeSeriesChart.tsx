import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { useTranslation } from 'react-i18next';

import { ChartTooltip } from '@/components/common/ChartTooltip';
import { chartTextClass, formatTimeTick, gridProps, xAxisProps, yAxisProps } from '@/components/common/chartTheme';
import { ChartLegend } from '@/components/common/ChartLegend';
import { formatBytes, formatNumber } from '@/lib/format';

import { LoadChart } from './LoadChart';

import type { MonitoringPoint, NodeEngine } from '@/api/types';

export interface NodeSeriesChartProps {
  points: MonitoringPoint[];
  /** Three colours from the page's palette: first series, second series, and the load chart's third line. */
  colors: [string, string, string];
  /** Which proxy produced these readings - it decides how many series they are. */
  engine?: NodeEngine;
}

const CHART_HEIGHT = 140;

export function NodeSeriesChart({ points, colors, engine = 'tproxy' }: NodeSeriesChartProps) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const [first, second] = colors;

  // telemt counts one thing where tproxy counts two.
  const telemt = engine === 'telemt';
  const countName = telemt ? t('monitoring.connections_live') : t('monitoring.sessions_live');
  const trafficName = telemt ? t('monitoring.traffic_total') : t('monitoring.traffic_down');

  const countLegend = [
    { key: 'sessions_live', name: countName, color: first },
    ...(telemt ? [] : [{ key: 'streams_live', name: t('monitoring.streams_live'), color: second }]),
  ];
  const rateLegend = [
    ...(telemt ? [] : [{ key: 'bytes_up_rate', name: t('monitoring.traffic_up'), color: first }]),
    { key: 'bytes_down_rate', name: trafficName, color: telemt ? first : second },
  ];

  return (
    <div className="space-y-4">
      <div>
        <div className={chartTextClass}>
          <ResponsiveContainer width="100%" height={CHART_HEIGHT}>
            <LineChart data={points} margin={{ top: 4, right: 24, bottom: 0, left: 0 }}>
              <CartesianGrid {...gridProps} />
              <XAxis {...xAxisProps} tickFormatter={(v: string) => formatTimeTick(v, locale)} />
              <YAxis {...yAxisProps} allowDecimals={false} width={52} />
              <Tooltip
                content={<ChartTooltip locale={locale} formatValue={(v) => formatNumber(v, locale)} />}
                cursor={{ stroke: 'var(--line-2)', strokeWidth: 1 }}
                isAnimationActive={false}
              />
              <Line
                type="monotone"
                dataKey="sessions_live"
                name={countName}
                stroke={first}
                strokeWidth={1.6}
                dot={false}
                activeDot={{ r: 3, strokeWidth: 0 }}
                connectNulls
                isAnimationActive={false}
              />
              {!telemt && (
                <Line
                  type="monotone"
                  dataKey="streams_live"
                  name={t('monitoring.streams_live')}
                  stroke={second}
                  strokeWidth={1.6}
                  dot={false}
                  activeDot={{ r: 3, strokeWidth: 0 }}
                  connectNulls
                  isAnimationActive={false}
                />
              )}
            </LineChart>
          </ResponsiveContainer>
        </div>
        <ChartLegend items={countLegend} className="mt-2" />
      </div>

      <div>
        <div className={chartTextClass}>
          <ResponsiveContainer width="100%" height={CHART_HEIGHT}>
            <LineChart data={points} margin={{ top: 4, right: 24, bottom: 0, left: 0 }}>
              <CartesianGrid {...gridProps} />
              <XAxis {...xAxisProps} tickFormatter={(v: string) => formatTimeTick(v, locale)} />
              <YAxis {...yAxisProps} width={52} tickFormatter={(v: number) => formatBytes(v, 0)} />
              <Tooltip
                content={<ChartTooltip locale={locale} formatValue={(v) => `${formatBytes(v)}/s`} />}
                cursor={{ stroke: 'var(--line-2)', strokeWidth: 1 }}
                isAnimationActive={false}
              />
              {!telemt && (
                <Line
                  type="monotone"
                  dataKey="bytes_up_rate"
                  name={t('monitoring.traffic_up')}
                  stroke={first}
                  strokeWidth={1.6}
                  dot={false}
                  activeDot={{ r: 3, strokeWidth: 0 }}
                  connectNulls
                  isAnimationActive={false}
                />
              )}
              <Line
                type="monotone"
                dataKey="bytes_down_rate"
                name={trafficName}
                stroke={telemt ? first : second}
                strokeWidth={1.6}
                dot={false}
                activeDot={{ r: 3, strokeWidth: 0 }}
                connectNulls
                isAnimationActive={false}
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
        <ChartLegend items={rateLegend} className="mt-2" />
      </div>

      <LoadChart points={points} colors={colors} height={CHART_HEIGHT} />
    </div>
  );
}
