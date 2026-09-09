import { describe, expect, it } from 'vitest';

import { fleetVerdict } from './verdict';

import type { FleetFacts } from './verdict';

const fleet = (facts: Partial<FleetFacts> = {}): FleetFacts => ({
  online: 3,
  total: 3,
  offline: 0,
  degraded: 0,
  attention: 0,
  ...facts,
});

describe('fleetVerdict', () => {
  it('calls it operational when every node reports and nothing is open', () => {
    expect(fleetVerdict(fleet())).toEqual({ status: 'healthy', headline: 'dashboard.verdict_ok' });
  });

  it('flags an open problem even while every node is reporting', () => {
    expect(fleetVerdict(fleet({ attention: 2 }))).toEqual({
      status: 'degraded',
      headline: 'dashboard.verdict_attention',
    });
  });

  it('prefers the specific fault to the generic one', () => {
    expect(fleetVerdict(fleet({ online: 2, offline: 1, attention: 1 }))).toEqual({
      status: 'degraded',
      headline: 'dashboard.verdict_nodes_down',
    });
  });

  it('names an unreported node even after its alert was dismissed', () => {
    expect(fleetVerdict(fleet({ online: 2, offline: 1 }))).toEqual({
      status: 'degraded',
      headline: 'dashboard.verdict_nodes_down',
    });
  });

  it('says the whole fleet is down when nothing is online', () => {
    expect(fleetVerdict(fleet({ online: 0, offline: 3, attention: 1 }))).toEqual({
      status: 'offline',
      headline: 'dashboard.verdict_all_down',
    });
  });

  it('reports partly answering nodes when none is fully down', () => {
    expect(fleetVerdict(fleet({ degraded: 1 }))).toEqual({
      status: 'degraded',
      headline: 'dashboard.verdict_nodes_degraded',
    });
  });

  it('infers the offline count when the summary does not carry one', () => {
    expect(fleetVerdict(fleet({ online: 1, offline: undefined }))).toEqual({
      status: 'degraded',
      headline: 'dashboard.verdict_nodes_down',
    });
  });

  it.each([[null], [undefined]])('admits it knows nothing when the fleet size is %s', (total) => {
    expect(fleetVerdict(fleet({ total }))).toEqual({ status: 'unknown', headline: 'dashboard.verdict_unknown' });
  });

  it('does not claim all-clear while the problem list is missing', () => {
    expect(fleetVerdict(fleet({ attention: undefined }))).toEqual({
      status: 'unknown',
      headline: 'dashboard.verdict_partial',
    });
  });
});
