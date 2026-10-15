import { describe, expect, it } from 'vitest';

import { formatSort, nextSort, parseSort, sortNodes } from './nodeSort';

import type { Node } from '@/api/types';

const node = (name: string, people: number | null, telemt: string, seen: string | null, mem?: number) =>
  ({
    id: name,
    name,
    status: seen ? 'online' : 'offline',
    engine: 'telemt',
    telemt_version: telemt,
    tproxy_version: '',
    people_online: people,
    last_seen_at: seen,
    health: mem === undefined ? undefined : { mem_used_percent: mem, cpu_utilisation_percent: mem / 2 },
  }) as unknown as Node;

const nodes = [
  node('France 1', 3, '3.5.9', '2026-10-15T10:00:00Z', 40),
  node('ComfyPublic 1', 120, '3.5.14', '2026-10-15T10:00:05Z', 20),
  node('props', null, '', null),
  node('ComfyPublic 2', 80, '3.5.14', '2026-10-15T09:59:00Z', 60),
];

const names = (list: Node[]) => list.map((n) => n.name);

describe('nextSort', () => {
  it('cycles natural direction, reversed, then the shared order', () => {
    const first = nextSort(null, 'people');
    expect(first).toEqual({ key: 'people', dir: 'desc' });
    const second = nextSort(first, 'people');
    expect(second).toEqual({ key: 'people', dir: 'asc' });
    expect(nextSort(second, 'people')).toBeNull();
    expect(nextSort(second, 'name')).toEqual({ key: 'name', dir: 'asc' });
  });

  it('round-trips through the URL value and ignores junk', () => {
    expect(parseSort(formatSort({ key: 'ram', dir: 'desc' }))).toEqual({ key: 'ram', dir: 'desc' });
    expect(parseSort('bogus:desc')).toBeNull();
    expect(parseSort('name:sideways')).toBeNull();
    expect(parseSort(null)).toBeNull();
  });
});

describe('sortNodes', () => {
  it('keeps the shared order without a sort', () => {
    expect(sortNodes(nodes, null, 'ru')).toBe(nodes);
  });

  it('sorts people from most to fewest and keeps servers without data last both ways', () => {
    expect(names(sortNodes(nodes, { key: 'people', dir: 'desc' }, 'ru'))).toEqual([
      'ComfyPublic 1',
      'ComfyPublic 2',
      'France 1',
      'props',
    ]);
    expect(names(sortNodes(nodes, { key: 'people', dir: 'asc' }, 'ru'))).toEqual([
      'France 1',
      'ComfyPublic 2',
      'ComfyPublic 1',
      'props',
    ]);
  });

  it('sorts names naturally and versions numerically', () => {
    expect(names(sortNodes(nodes, { key: 'name', dir: 'asc' }, 'ru'))).toEqual([
      'ComfyPublic 1',
      'ComfyPublic 2',
      'France 1',
      'props',
    ]);
    expect(names(sortNodes(nodes, { key: 'versions', dir: 'desc' }, 'ru')).slice(0, 3)).toEqual([
      'ComfyPublic 1',
      'ComfyPublic 2',
      'France 1',
    ]);
  });

  it('sorts by memory and by last contact', () => {
    expect(names(sortNodes(nodes, { key: 'ram', dir: 'desc' }, 'ru'))).toEqual([
      'ComfyPublic 2',
      'France 1',
      'ComfyPublic 1',
      'props',
    ]);
    expect(names(sortNodes(nodes, { key: 'seen', dir: 'desc' }, 'ru'))).toEqual([
      'ComfyPublic 1',
      'France 1',
      'ComfyPublic 2',
      'props',
    ]);
  });
});
