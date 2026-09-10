import type { CheckStatus, DiagnosticGroup } from '@/api/types';
import type { StatTone } from '@/components/common/statTone';

/**
 * A pass counted the way the panel reports it: checks that could not run are their own number
 * and are in neither `passed` nor `total`, so "9 / 9" never covers for a check that was skipped.
 */
export interface DiagnosticsTally {
  passed: number;
  warned: number;
  failed: number;
  notRun: number;
  total: number;
}

export function tallyChecks(groups: DiagnosticGroup[] | undefined): DiagnosticsTally {
  const tally: DiagnosticsTally = { passed: 0, warned: 0, failed: 0, notRun: 0, total: 0 };
  for (const group of groups ?? []) {
    for (const check of group.checks ?? []) {
      switch (check.status) {
        case 'not_available':
          tally.notRun += 1;
          continue;
        case 'ok':
          tally.passed += 1;
          break;
        case 'warn':
          tally.warned += 1;
          break;
        case 'fail':
          tally.failed += 1;
          break;
      }
      tally.total += 1;
    }
  }
  return tally;
}

export const CHECK_TONE: Record<CheckStatus, StatTone> = {
  ok: 'ok',
  warn: 'warn',
  fail: 'err',
  not_available: 'neutral',
};
