import { describe, expect, it } from 'vitest';

import { currentThroughput, daysLeft, inServerOrder, nearestCertificate, worstRoute } from './fleetFacts';

import type { MonitoringNode, MonitoringPoint, Node } from '@/api/types';

const DAY = 86_400_000;
const NOW = Date.UTC(2026, 9, 1, 12);

function node(name: string, status: Node['status'], dcs: { dc: number; latency_ms: number; known: boolean }[]): Node {
  return { id: name, name, status, health: { dc_data_available: true, dcs } } as unknown as Node;
}

function entry(id: string, cert?: number): MonitoringNode {
  return {
    node_id: id,
    node_name: id,
    hostname: id,
    status: 'online',
    cert_expires_at: cert === undefined ? null : new Date(NOW + cert * DAY + 3_600_000).toISOString(),
  };
}

describe('worstRoute', () => {
  it('finds the slowest known DC on a reachable server', () => {
    const nodes = [
      node('Amsterdam', 'online', [{ dc: 2, latency_ms: 120, known: true }]),
      node('Frankfurt', 'online', [
        { dc: 4, latency_ms: 90, known: true },
        { dc: 5, latency_ms: 188, known: true },
        { dc: 1, latency_ms: 900, known: false },
      ]),
      node('Helsinki', 'offline', [{ dc: 3, latency_ms: 999, known: true }]),
    ];
    expect(worstRoute(nodes)).toEqual({ ms: 188, node: 'Frankfurt', dc: 5 });
  });

  it('has nothing to say without DC data', () => {
    expect(worstRoute([node('Amsterdam', 'online', [])])).toBeNull();
  });
});

describe('certificates', () => {
  it('counts whole days left and goes negative once expired', () => {
    expect(daysLeft(new Date(NOW + 61.5 * DAY).toISOString(), NOW)).toBe(61);
    expect(daysLeft(new Date(NOW - 0.5 * DAY).toISOString(), NOW)).toBe(-1);
  });

  it('picks the certificate that expires first among the ones read', () => {
    expect(nearestCertificate([entry('a', 61), entry('b'), entry('c', 12)], NOW)).toEqual({ days: 12, node: 'c' });
    expect(nearestCertificate([entry('b')], NOW)).toBeNull();
  });
});

describe('currentThroughput', () => {
  it('adds the last rate of each server and stays unknown without one', () => {
    const point = (up: number | null, down: number | null) => ({ bytes_up_rate: up, bytes_down_rate: down }) as MonitoringPoint;
    expect(currentThroughput({ a: [point(1, 1), point(10, 20)], b: [point(5, 5)] })).toBe(40);
    expect(currentThroughput({ a: [point(null, null)] })).toBeNull();
  });
});

describe('inServerOrder', () => {
  it('follows the servers list, not the names', () => {
    const ordered = inServerOrder([entry('a'), entry('b'), entry('c')], [{ id: 'c' }, { id: 'a' }]);
    expect(ordered.map((e) => e.node_id)).toEqual(['c', 'a', 'b']);
  });
});
