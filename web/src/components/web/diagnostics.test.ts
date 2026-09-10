import { describe, expect, it } from 'vitest';

import { tallyChecks } from './diagnostics';

import type { CheckStatus, DiagnosticGroup } from '@/api/types';

const group = (key: string, statuses: CheckStatus[]): DiagnosticGroup => ({
  key,
  checks: statuses.map((status, i) => ({ key: `${key}_${i}`, status, value: null, detail: null })),
});

describe('tallyChecks', () => {
  it('counts a check that could not run as neither passed nor failed', () => {
    const tally = tallyChecks([group('web_transport', ['ok', 'not_available', 'ok', 'not_available'])]);

    expect(tally).toEqual({ passed: 2, warned: 0, failed: 0, notRun: 2, total: 2 });
  });

  it('keeps failures and warnings apart and out of the passed count', () => {
    const tally = tallyChecks([group('dns', ['ok', 'fail']), group('telemt', ['warn', 'not_available'])]);

    expect(tally.passed).toBe(1);
    expect(tally.failed).toBe(1);
    expect(tally.warned).toBe(1);
    expect(tally.notRun).toBe(1);
    expect(tally.total).toBe(3);
  });

  it('reports a pass where nothing ran as zero of zero, not as healthy', () => {
    const tally = tallyChecks([group('web_transport', ['not_available', 'not_available'])]);

    expect(tally.passed).toBe(0);
    expect(tally.total).toBe(0);
    expect(tally.notRun).toBe(2);
  });

  it('survives an empty or missing set of groups', () => {
    expect(tallyChecks(undefined).total).toBe(0);
    expect(tallyChecks([]).total).toBe(0);
  });
});
