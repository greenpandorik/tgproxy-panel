// recharts is only ever imported here - MonitoringPage lazy-loads this
// component (React.lazy) so the shell/other pages' bundle stays free of it.
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

/**
 * One node's three readings, stacked and sharing an x-axis: how many sessions
 * and streams are open, how fast bytes are moving, and how loaded the box
 * underneath is. They are three charts and not one because they are three
 * units - a session count, a byte rate and a percentage on one pair of axes
 * would only invite the reader to compare numbers that cannot be compared.
 */
export function NodeSeriesChart({ points, colors, engine = 'tproxy' }: NodeSeriesChartProps) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const [first, second] = colors;

  /*
   * telemt counts one thing where tproxy counts two. It has no streams inside a
   * session (the worker writes the live connection count into both columns) and
   * it reports a single cumulative octet counter for both directions, which the
   * worker lands in bytes_down. Drawing the tproxy pair for it would put two
   * identical lines on the sessions chart and a permanently flat zero on the
   * traffic one, so a telemt node gets one line per chart, named for what it
   * actually is: connections, and traffic.
   */
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
