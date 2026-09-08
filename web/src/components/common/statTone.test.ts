import { describe, expect, it } from 'vitest';

import { LOAD_ERR_PERCENT, LOAD_WARN_PERCENT, statTone } from './statTone';

describe('statTone', () => {
  it('keeps a fact with no state neutral', () => {
    expect(statTone({ kind: 'stateless' })).toBe('neutral');
  });

  it('is ok while every node reports and warns as soon as one does not', () => {
    expect(statTone({ kind: 'nodes_online', online: 3, total: 3 })).toBe('ok');
    expect(statTone({ kind: 'nodes_online', online: 2, total: 3 })).toBe('warn');
    expect(statTone({ kind: 'nodes_online', online: 0, total: 3 })).toBe('warn');
  });

  it('leaves an empty fleet neutral rather than calling it healthy', () => {
    expect(statTone({ kind: 'nodes_online', online: 0, total: 0 })).toBe('neutral');
  });

  it('carries a fault tone only while the fault exists', () => {
    expect(statTone({ kind: 'nodes_offline', count: 0 })).toBe('neutral');
    expect(statTone({ kind: 'nodes_offline', count: 1 })).toBe('err');

    expect(statTone({ kind: 'degraded', count: 0 })).toBe('neutral');
    expect(statTone({ kind: 'degraded', count: 2 })).toBe('warn');

    expect(statTone({ kind: 'alerts_open', count: 0 })).toBe('neutral');
    expect(statTone({ kind: 'alerts_open', count: 1 })).toBe('warn');

    expect(statTone({ kind: 'keys_pending', count: 0 })).toBe('neutral');
    expect(statTone({ kind: 'keys_pending', count: 4 })).toBe('info');
  });

  it('steps average load ok, then warn, then error', () => {
    expect(statTone({ kind: 'avg_load', percent: 0 })).toBe('ok');
    expect(statTone({ kind: 'avg_load', percent: LOAD_WARN_PERCENT - 0.1 })).toBe('ok');
    expect(statTone({ kind: 'avg_load', percent: LOAD_WARN_PERCENT })).toBe('warn');
    expect(statTone({ kind: 'avg_load', percent: LOAD_ERR_PERCENT - 0.1 })).toBe('warn');
    expect(statTone({ kind: 'avg_load', percent: LOAD_ERR_PERCENT })).toBe('err');
    expect(statTone({ kind: 'avg_load', percent: 100 })).toBe('err');
  });

  it('has no tone for a load nothing reported', () => {
    expect(statTone({ kind: 'avg_load', percent: null })).toBe('neutral');
    expect(statTone({ kind: 'avg_load', percent: Number.NaN })).toBe('neutral');
  });
});
