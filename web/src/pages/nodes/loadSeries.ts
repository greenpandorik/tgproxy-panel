import { isMetricPresent } from '@/components/common/metric';

import type { NodeEngine, SeriesPoint } from '@/api/types';

/** One moment of a server's load: who was on it, how busy the processor was and how fast traffic moved. */
export interface LoadRow {
  t: string;
  /** People online on telemt, live sessions on tproxy. */
  people: number | null;
  cpu: number | null;
  /** Bytes per second since the previous reading. */
  traffic: number | null;
}

function bytesOf(point: SeriesPoint, engine: NodeEngine): number | null {
  const down = point.bytes_down as number | null | undefined;
  const up = point.bytes_up as number | null | undefined;
  if (!isMetricPresent(down)) return null;
  if (engine === 'telemt') return down;
  return isMetricPresent(up) ? up + down : null;
}

export function loadRows(points: SeriesPoint[], engine: NodeEngine): LoadRow[] {
  return points.map((point, i) => {
    const people = engine === 'telemt' ? point.people_online : point.sessions_live;
    let traffic: number | null = null;
    if (i > 0) {
      const prev = bytesOf(points[i - 1], engine);
      const cur = bytesOf(point, engine);
      const seconds = (Date.parse(point.t) - Date.parse(points[i - 1].t)) / 1000;
      if (prev !== null && cur !== null && cur >= prev && seconds > 0) traffic = (cur - prev) / seconds;
    }
    return {
      t: point.t,
      people: isMetricPresent(people) ? people : null,
      cpu: isMetricPresent(point.cpu_utilisation_percent) ? point.cpu_utilisation_percent : null,
      traffic,
    };
  });
}

/** The newest reading of one figure, skipping the moments it was not measured. */
export function latest(rows: LoadRow[], key: 'people' | 'cpu' | 'traffic'): number | null {
  for (let i = rows.length - 1; i >= 0; i -= 1) {
    const v = rows[i][key];
    if (v !== null) return v;
  }
  return null;
}
