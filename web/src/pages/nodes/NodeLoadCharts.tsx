import { useId } from 'react';
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
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
import { formatBytes, formatNumber } from '@/lib/format';

import { DASH } from './nodeDisplay';
import { latest } from './loadSeries';

import type { LoadRow } from './loadSeries';
import type { NodeEngine } from '@/api/types';

const HEIGHT = 112;
const COLOR = 'var(--series-1)';

interface MiniChartProps {
  rows: LoadRow[];
  dataKey: 'people' | 'cpu' | 'traffic';
  name: string;
  format: (value: number) => string;
  axisFormat?: (value: number) => string;
  domain?: [number, number | 'auto'];
  ticks?: number[];
  syncId: string;
}

function MiniChart({ rows, dataKey, name, format, axisFormat, domain = [0, 'auto'], ticks, syncId }: MiniChartProps) {
  const { t, i18n } = useTranslation();
  const now = latest(rows, dataKey);
  const label = now === null ? DASH : format(now);
  return (
    <figure className="min-w-0" aria-label={t('nodes.detail_chart_now', { name, value: label })}>
      <figcaption className="mb-2 flex items-baseline justify-between gap-3">
        <span className="truncate text-label text-mute">{name}</span>
        <span className="mono shrink-0 text-mono text-foreground">{label}</span>
      </figcaption>
      <div className={chartTextClass}>
        <ResponsiveContainer width="100%" height={HEIGHT}>
          <AreaChart data={rows} syncId={syncId} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
            <CartesianGrid {...gridProps} />
            <XAxis {...xAxisProps} tickFormatter={(v: string) => formatTimeTick(v, i18n.language)} />
            <YAxis
              {...yAxisProps}
              width={52}
              domain={domain}
              ticks={ticks}
              tickCount={3}
              allowDecimals={false}
              tickFormatter={axisFormat ?? format}
            />
            <Tooltip
              content={<ChartTooltip locale={i18n.language} formatValue={format} />}
              cursor={{ stroke: 'var(--line-2)', strokeWidth: 1 }}
              isAnimationActive={false}
            />
            <Area
              type="monotone"
              dataKey={dataKey}
              name={name}
              stroke={COLOR}
              strokeWidth={1.6}
              fill={COLOR}
              fillOpacity={0.1}
              dot={false}
              activeDot={{ r: 3, strokeWidth: 0 }}
              isAnimationActive={false}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </figure>
  );
}

/** A server's people, processor and traffic over the same window, side by side on a wide screen. */
export function NodeLoadCharts({ rows, engine }: { rows: LoadRow[]; engine: NodeEngine }) {
  const { t, i18n } = useTranslation();
  const syncId = useId();
  const lang = i18n.language;
  const maxTraffic = Math.max(0, ...rows.map((r) => r.traffic ?? 0));
  return (
    <div className="grid gap-6 md:grid-cols-3">
      <MiniChart
        rows={rows}
        dataKey="people"
        name={engine === 'telemt' ? t('common.people_online') : t('monitoring.sessions_live')}
        format={(v) => formatNumber(Math.round(v), lang)}
        syncId={syncId}
      />
      <MiniChart
        rows={rows}
        dataKey="cpu"
        name={t('nodes.detail_chart_cpu')}
        format={(v) => `${Math.round(v)}%`}
        domain={[0, 100]}
        ticks={[0, 50, 100]}
        syncId={syncId}
      />
      <MiniChart
        rows={rows}
        dataKey="traffic"
        name={t('monitoring.traffic_total')}
        format={(v) => t('common.per_second', { value: formatBytes(v, 1, lang) })}
        axisFormat={bytesAxisFormatter(maxTraffic, lang)}
        syncId={syncId}
      />
    </div>
  );
}
