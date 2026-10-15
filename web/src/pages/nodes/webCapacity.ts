import type { Node, WebCapacity } from '@/api/types';

/** Mirrors internal/domain/webcapacity.go: WEB sessions and server.max_connections per preset. */
export const WEB_CAPACITY_PRESETS: readonly { people: WebCapacity; connections: number }[] = [
  { people: 128, connections: 10000 },
  { people: 256, connections: 15000 },
  { people: 512, connections: 20000 },
  { people: 1024, connections: 40000 },
  { people: 2048, connections: 80000 },
];

/** telemt's own defaults: 128 WEB sessions and 10 000 TCP connections. */
export const STANDARD_WEB_CAPACITY: WebCapacity = 128;
export const STANDARD_MAX_CONNECTIONS = 10000;

export const isWebCapacity = (n: number | undefined): n is WebCapacity => WEB_CAPACITY_PRESETS.some((p) => p.people === n);

/** The capacity for the settings row: the reported value, else the chosen one, else telemt's default. */
export function webCapacityPeople(node: Node): number {
  return node.health?.web_capacity ?? node.web_capacity ?? STANDARD_WEB_CAPACITY;
}
