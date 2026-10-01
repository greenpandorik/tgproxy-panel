import { beforeEach, describe, expect, it } from 'vitest';

import i18n, { setLang } from '@/i18n';

import { checkTally, dcState, serviceReadings } from './nodeHealth';
import { checksSummary, dcSummary, servicesSummary, webSummary } from './sectionSummaries';

import type { DiagnosticsRun, NodeCheckReport, NodeHealth, WebCarrierStats } from '@/api/types';

const t = i18n.t;

const health = (over: Partial<NodeHealth> = {}) =>
  ({
    relay_active: true,
    mtproxy_active: true,
    caddy_active: true,
    healthz: true,
    readyz: true,
    disk_used_percent: 41,
    upstream_healthy: true,
    dc_data_available: true,
    dcs: [1, 2, 3, 4, 5].map((dc) => ({ dc, latency_ms: 40 + dc, known: true, ip_preference: 'ipv4' })),
    ...over,
  }) as NodeHealth;

describe('section summaries', () => {
  beforeEach(() => setLang('ru'));

  it('sums up the datacenters', () => {
    const h = health();
    expect(dcSummary('ready', h, dcState(h), t)).toEqual({ text: 'все 5 доступны', tone: 'ok' });
    const two = health({ dcs: h.dcs!.map((d, i) => (i < 2 ? { ...d, known: false } : d)) });
    expect(dcSummary('ready', two, dcState(two), t)).toEqual({ text: '2 недоступны', tone: 'warn' });
    const down = health({ upstream_healthy: false });
    expect(dcSummary('ready', down, dcState(down), t).tone).toBe('err');
    expect(dcSummary('offline', undefined, null, t)).toEqual({ text: 'сервер не на связи', tone: 'neutral' });
  });

  it('names the services that run, or the ones that do not', () => {
    const node = { engine: 'telemt' as const, online: true };
    const h = health();
    expect(servicesSummary('ready', h, serviceReadings(node, h, 'агент'), 'telemt', t)).toEqual({
      text: 'telemt, caddy, агент работают',
      tone: 'ok',
    });
    const caddyDown = health({ caddy_active: false });
    expect(servicesSummary('ready', caddyDown, serviceReadings(node, caddyDown, 'агент'), 'telemt', t)).toEqual({
      text: 'не работает: caddy',
      tone: 'err',
    });
  });

  it('counts every check the server had', () => {
    const run = {
      groups: [
        {
          key: 'dns',
          checks: [
            { key: 'a', status: 'ok' },
            { key: 'b', status: 'fail' },
            { key: 'c', status: 'not_available' },
          ],
        },
      ],
      started_at: '2026-10-01T10:00:00Z',
      finished_at: '2026-10-01T10:00:05Z',
    } as unknown as DiagnosticsRun;
    const report = {
      ran_at: '2026-10-01T11:00:00Z',
      all_ok: true,
      results: [
        { name: 'dns_a', ok: true, detail: '' },
        { name: 'pq_kex', ok: false, advisory: true, detail: '' },
      ],
    } as NodeCheckReport;
    const tally = checkTally(run, report, [{ status: 'ok' }, { status: 'not_run' }]);
    expect(tally).toMatchObject({ passed: 3, failed: 1, total: 4, ranAt: '2026-10-01T11:00:00Z' });
    expect(checksSummary(tally, t)).toEqual({ text: 'не пройдено: 1 из 4', tone: 'err' });
    expect(checksSummary(checkTally(undefined, null), t)).toEqual({ text: 'ещё не проводились', tone: 'neutral' });
  });

  it('says how many ways to connect the WEB transport used and whether anything failed', () => {
    const stats = {
      carrier_selection_distribution: [
        { carrier: 'websocket', selections: 10, share: 0.5 },
        { carrier: 'https-lanes', selections: 6, share: 0.3 },
        { carrier: 'https', selections: 4, share: 0.2 },
      ],
      carrier_failures: 0,
      rejected_attempts: 0,
      evicted_sessions: 0,
    } as unknown as WebCarrierStats;
    expect(webSummary('supported', health(), stats, t)).toEqual({ text: '3 способа связи, ошибок нет', tone: 'neutral' });
    const paused = health({ web_runtime: { lifecycle: { state: 'paused' } } as NodeHealth['web_runtime'] });
    expect(webSummary('supported', paused, stats, t).tone).toBe('warn');
  });
});
