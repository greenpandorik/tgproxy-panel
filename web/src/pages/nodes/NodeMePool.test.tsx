import { render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import '@/i18n';
import { setLang } from '@/i18n';
import type { ReliabilityReport } from '@/api/reliability';
import { ReliabilityRoutes } from './NodeReliability';

const state = {
  active_generation: 4,
  pending_generation: 5,
  pending_age_seconds: 80,
  hardswap_enabled: true,
  pending: true,
  pending_map_current: true,
  pending_writers: 3,
  writer_deficit: 2,
  missing_dc_groups: 1,
  orphan_warm_writers: 0,
  replacement_preparing: 0,
  replacement_retiring: 0,
  orphan_observed_seconds: 0,
  replacement_observed_seconds: 0,
};
function report(me_pool: unknown): ReliabilityReport {
  return { version: 1, at: '2026-10-11T10:00:00Z', routes: [], me_pool } as unknown as ReliabilityReport;
}
beforeEach(() => {
  setLang('en');
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-10-11T10:00:30Z'));
});
afterEach(() => vi.useRealTimers());
it('shows fresh pool transition and its actual writer deficit', () => {
  render(<ReliabilityRoutes report={report({ at: '2026-10-11T10:00:00Z', enabled: true, state })} />);
  expect(screen.getByText('Middle Proxy pool')).toBeVisible();
  expect(screen.getByText('Pool transition in progress')).toBeVisible();
  const deficit = screen.getByText('Writer deficit').closest('div')!;
  expect(within(deficit).getByText('2')).toBeVisible();
  expect(screen.queryByText('Pool is stable')).not.toBeInTheDocument();
});
it('renders an absent counter as unavailable instead of zero', () => {
  render(
    <ReliabilityRoutes
      report={report({ at: '2026-10-11T10:00:00Z', enabled: true, state: { ...state, writer_deficit: null } })}
    />,
  );
  const deficit = screen.getByText('Writer deficit').closest('div')!;
  expect(within(deficit).getByText('Not available')).toBeVisible();
  expect(within(deficit).queryByText('0')).not.toBeInTheDocument();
});
it('does not display stale pool counters as current readings', () => {
  render(<ReliabilityRoutes report={report({ at: '2026-10-11T09:55:00Z', enabled: true, state })} />);
  expect(screen.getByText('The pool reading is stale. Wait for a fresh agent report.')).toBeVisible();
  expect(screen.queryByText('Writer deficit')).not.toBeInTheDocument();
});
it('does not claim healthy pool state when the engine has no snapshot', () => {
  render(
    <ReliabilityRoutes
      report={report({ at: '2026-10-11T10:00:00Z', enabled: false, reason: 'source_unavailable', state: null })}
    />,
  );
  expect(screen.getByText('Middle Proxy diagnostics are unavailable on this node.')).toBeVisible();
  expect(screen.queryByText('Pool is stable')).not.toBeInTheDocument();
});
it('keeps legacy reports usable without fabricating pool data', () => {
  render(<ReliabilityRoutes report={report(undefined)} />);
  expect(screen.queryByText('Pool is stable')).not.toBeInTheDocument();
});
