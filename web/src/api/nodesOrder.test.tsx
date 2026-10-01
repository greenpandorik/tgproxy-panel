import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { nodeKeys, reorderedNodes, useReorderNodes } from './nodes';

import type { Node, Paginated } from './types';
import type { ReactNode } from 'react';

const node = (id: string) => ({ id, name: id.toUpperCase() }) as Node;
const list = (...ids: string[]): Paginated<Node> => ({ items: ids.map(node), total: ids.length }) as Paginated<Node>;
const order = (qc: QueryClient) => qc.getQueryData<Paginated<Node>>(nodeKeys.all)?.items.map((n) => n.id);

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  qc.setQueryData(nodeKeys.all, list('a', 'b', 'c'));
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  return { qc, ...renderHook(() => useReorderNodes(), { wrapper }) };
}

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

describe('reorderedNodes', () => {
  it('puts the listed servers first and keeps the rest in their order', () => {
    expect(reorderedNodes(list('a', 'b', 'c', 'd').items, ['c', 'a']).map((n) => n.id)).toEqual(['c', 'a', 'b', 'd']);
    expect(reorderedNodes(list('a', 'b').items, ['zz', 'b']).map((n) => n.id)).toEqual(['b', 'a']);
  });
});

describe('useReorderNodes', () => {
  let release: (() => void) | undefined;

  beforeEach(() => {
    release = undefined;
  });

  it('moves the list at once, before the server answers', async () => {
    globalThis.fetch = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return new Promise<Response>((resolve) => {
          release = () => void json({ ids: ['c', 'a', 'b'] }).then(resolve);
        });
      }
      return json(list('c', 'a', 'b'));
    }) as typeof fetch;
    const { qc, result } = setup();

    act(() => result.current.mutate(['c', 'a', 'b']));
    await waitFor(() => expect(order(qc)).toEqual(['c', 'a', 'b']));
    expect(result.current.isPending).toBe(true);

    act(() => release?.());
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(order(qc)).toEqual(['c', 'a', 'b']);
  });

  it('puts the list back when saving fails', async () => {
    const calls: string[] = [];
    globalThis.fetch = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      calls.push(init?.method ?? 'GET');
      if (init?.method === 'PUT') return json({ error: { code: 'internal', message: 'boom' } }, 500);
      return new Promise<Response>(() => {});
    }) as typeof fetch;
    const { qc, result } = setup();

    act(() => result.current.mutate(['b', 'c', 'a']));
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(order(qc)).toEqual(['a', 'b', 'c']);
    expect(calls).toContain('PUT');
  });
});
