import { describe, expect, it } from 'vitest';

import {
  DC_ERR_MS,
  DC_WARN_MS,
  dcLatencyOf,
  dcLatencyRows,
  dcSeriesColor,
  dcSeriesKey,
  dcTone,
  meanDcLatency,
  nodeDcLatency,
  routeTone,
} from './dcDisplay';

import type { Node, NodeHealth, SeriesPoint } from '@/api/types';

function health(extra: Partial<NodeHealth> = {}): NodeHealth {
  return {
    relay_active: true,
    mtproxy_active: true,
    caddy_active: true,
    healthz: true,
    readyz: true,
    tproxy_version: 'telemt 3.5.7',
    agent_version: '1',
    uptime_seconds: 10,
    cpu_percent: 1,
    mem_used_percent: 1,
    disk_used_percent: 1,
    profile_count: 0,
    ...extra,
  };
}

function node(status: Node['status'], h?: NodeHealth): Pick<Node, 'status' | 'health'> {
  return { status, health: h };
}

function point(t: string, dc_latency?: Record<string, number>): SeriesPoint {
  return {
    t,
    cpu_percent: 0,
    mem_used_percent: 0,
    disk_used_percent: 0,
    sessions_live: 0,
    streams_live: 0,
    bytes_up: 0,
    bytes_down: 0,
    ...(dc_latency ? { dc_latency } : {}),
  };
}

describe('dcTone', () => {
  it('has no tone for a latency nobody measured', () => {
    expect(dcTone(null)).toBe('neutral');
    expect(dcTone(Number.NaN)).toBe('neutral');
    expect(dcTone(Number.POSITIVE_INFINITY)).toBe('neutral');
  });

  it('is ok below the warn threshold, including zero', () => {
    expect(dcTone(0)).toBe('ok');
    expect(dcTone(36.5)).toBe('ok');
    expect(dcTone(DC_WARN_MS - 0.1)).toBe('ok');
  });

  it('warns from the warn threshold up to the error threshold', () => {
    expect(dcTone(DC_WARN_MS)).toBe('warn');
    expect(dcTone(197.9)).toBe('warn');
    expect(dcTone(DC_ERR_MS - 0.1)).toBe('warn');
  });

  it('is an error from the error threshold on', () => {
    expect(dcTone(DC_ERR_MS)).toBe('err');
    expect(dcTone(2500)).toBe('err');
  });

  it('pins the thresholds the spec names', () => {
    expect(DC_WARN_MS).toBe(150);
    expect(DC_ERR_MS).toBe(400);
  });
});

describe('routeTone', () => {
  it('is an error whenever the route is unhealthy, whatever the fail count says', () => {
    expect(routeTone({ healthy: false, fails: 0 })).toBe('err');
    expect(routeTone({ healthy: false, fails: 3 })).toBe('err');
  });

  it('warns on a healthy route that has been failing', () => {
    expect(routeTone({ healthy: true, fails: 1 })).toBe('warn');
  });

  it('is ok on a healthy route with no fails', () => {
    expect(routeTone({ healthy: true, fails: 0 })).toBe('ok');
  });
});

describe('dcLatencyOf', () => {
  it('reads a measured DC and has nothing for one telemt has not measured', () => {
    expect(dcLatencyOf({ latency_ms: 197.9, known: true })).toBe(197.9);
    expect(dcLatencyOf({ latency_ms: 197.9, known: false })).toBeNull();
    expect(dcLatencyOf({ latency_ms: 0, known: true })).toBeNull();
    expect(dcLatencyOf({ latency_ms: Number.NaN, known: true })).toBeNull();
  });
});

describe('nodeDcLatency', () => {
  it('reads the effective latency of a reporting node', () => {
    expect(nodeDcLatency(node('online', health({ dc_data_available: true, effective_latency_ms: 42.4 })))).toBe(42.4);
    expect(nodeDcLatency(node('degraded', health({ dc_data_available: true, effective_latency_ms: 42.4 })))).toBe(42.4);
  });

  it('has nothing for an offline node, a node without health, or an old panel that omits the field', () => {
    expect(nodeDcLatency(node('offline', health({ dc_data_available: true, effective_latency_ms: 42 })))).toBeUndefined();
    expect(nodeDcLatency(node('online'))).toBeUndefined();
    expect(nodeDcLatency(node('online', health()))).toBeUndefined();
  });

  it('has nothing when the agent says the data is unavailable, or the figure is not a measurement', () => {
    expect(nodeDcLatency(node('online', health({ dc_data_available: false, effective_latency_ms: 42 })))).toBeUndefined();
    expect(nodeDcLatency(node('online', health({ dc_data_available: true, effective_latency_ms: 0 })))).toBeUndefined();
    expect(nodeDcLatency(node('online', health({ dc_data_available: true, effective_latency_ms: -1 })))).toBeUndefined();
    expect(nodeDcLatency(node('online', health({ dc_data_available: true, effective_latency_ms: Number.NaN })))).toBeUndefined();
  });
});

describe('meanDcLatency', () => {
  it('averages only the nodes that report a figure', () => {
    const nodes = [
      node('online', health({ dc_data_available: true, effective_latency_ms: 100 })),
      node('online', health({ dc_data_available: true, effective_latency_ms: 200 })),
      node('offline', health({ dc_data_available: true, effective_latency_ms: 900 })),
      node('online', health()),
    ];
    expect(meanDcLatency(nodes)).toBe(150);
  });

  it('is null when nothing reports', () => {
    expect(meanDcLatency([])).toBeNull();
    expect(meanDcLatency([node('online', health()), node('offline')])).toBeNull();
  });
});

describe('dcLatencyRows', () => {
  it('names one series per DC, in DC order, and keeps the timestamp on every row', () => {
    const { dcs, rows } = dcLatencyRows([
      point('2026-09-08T10:00:00Z', { '2': 36.5, '1': 197.9 }),
      point('2026-09-08T10:05:00Z', { '1': 190, '4': 80 }),
    ]);
    expect(dcs).toEqual([1, 2, 4]);
    expect(rows).toEqual([
      { t: '2026-09-08T10:00:00Z', [dcSeriesKey(1)]: 197.9, [dcSeriesKey(2)]: 36.5 },
      { t: '2026-09-08T10:05:00Z', [dcSeriesKey(1)]: 190, [dcSeriesKey(4)]: 80 },
    ]);
  });

  it('drops points that carry no DC figures, and figures that are not numbers', () => {
    const { dcs, rows } = dcLatencyRows([
      point('2026-09-08T10:00:00Z'),
      point('2026-09-08T10:05:00Z', {}),
      point('2026-09-08T10:10:00Z', { '1': Number.NaN, x: 5 }),
      point('2026-09-08T10:15:00Z', { '3': 12 }),
    ]);
    expect(dcs).toEqual([3]);
    expect(rows).toEqual([{ t: '2026-09-08T10:15:00Z', [dcSeriesKey(3)]: 12 }]);
  });

  it('is empty for an empty series', () => {
    expect(dcLatencyRows([])).toEqual({ dcs: [], rows: [] });
  });
});

describe('dcSeriesColor', () => {
  it('hands out the series tokens in order and wraps after the eighth', () => {
    expect(dcSeriesColor(0)).toBe('var(--series-1)');
    expect(dcSeriesColor(4)).toBe('var(--series-5)');
    expect(dcSeriesColor(7)).toBe('var(--series-8)');
    expect(dcSeriesColor(8)).toBe('var(--series-1)');
  });
});
