import { engineVersion, nodeLoad } from './nodeDisplay';

import type { Node } from '@/api/types';

export const SORT_KEYS = ['name', 'people', 'cpu', 'ram', 'versions', 'seen'] as const;
export type SortKey = (typeof SORT_KEYS)[number];
export type SortDir = 'asc' | 'desc';
export interface NodeSort {
  key: SortKey;
  dir: SortDir;
}

const FIRST_DIR: Record<SortKey, SortDir> = {
  name: 'asc',
  people: 'desc',
  cpu: 'desc',
  ram: 'desc',
  versions: 'desc',
  seen: 'desc',
};

export function parseSort(raw: string | null): NodeSort | null {
  if (!raw) return null;
  const [key, dir] = raw.split(':');
  if (!(SORT_KEYS as readonly string[]).includes(key) || (dir !== 'asc' && dir !== 'desc')) return null;
  return { key: key as SortKey, dir };
}

export function formatSort(sort: NodeSort | null): string | null {
  return sort ? `${sort.key}:${sort.dir}` : null;
}

/** First click sorts in the column's natural direction, the second reverses it, the third returns to the shared order. */
export function nextSort(current: NodeSort | null, key: SortKey): NodeSort | null {
  const first = FIRST_DIR[key];
  if (!current || current.key !== key) return { key, dir: first };
  if (current.dir === first) return { key, dir: first === 'asc' ? 'desc' : 'asc' };
  return null;
}

function versionParts(v: string): number[] {
  return v
    .replace(/^v/, '')
    .split(/[.-]/)
    .map((p) => Number.parseInt(p, 10))
    .filter((n) => Number.isFinite(n));
}

function compareVersions(a: string, b: string): number {
  const pa = versionParts(a);
  const pb = versionParts(b);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const d = (pa[i] ?? 0) - (pb[i] ?? 0);
    if (d !== 0) return d;
  }
  return 0;
}

type Value = number | string | undefined;

function valueOf(node: Node, key: SortKey): Value {
  switch (key) {
    case 'name':
      return node.name;
    case 'people':
      return node.people_online ?? undefined;
    case 'cpu':
      return nodeLoad(node)?.cpu;
    case 'ram':
      return nodeLoad(node)?.mem;
    case 'versions':
      return engineVersion(node) || undefined;
    case 'seen':
      return node.last_seen_at ? Date.parse(node.last_seen_at) : undefined;
  }
}

/** Servers sorted by a column; ones with no value for it stay last whichever way it is sorted. */
export function sortNodes(nodes: Node[], sort: NodeSort | null, locale: string): Node[] {
  if (!sort) return nodes;
  const sign = sort.dir === 'asc' ? 1 : -1;
  return nodes
    .map((node, index) => ({ node, index, value: valueOf(node, sort.key) }))
    .sort((a, b) => {
      if (a.value === undefined || b.value === undefined) {
        if (a.value === b.value) return a.index - b.index;
        return a.value === undefined ? 1 : -1;
      }
      let d: number;
      if (sort.key === 'versions') d = compareVersions(String(a.value), String(b.value));
      else if (typeof a.value === 'string')
        d = a.value.localeCompare(String(b.value), locale, { sensitivity: 'base', numeric: true });
      else d = a.value - (b.value as number);
      return d !== 0 ? d * sign : a.index - b.index;
    })
    .map((x) => x.node);
}
