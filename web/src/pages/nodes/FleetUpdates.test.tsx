import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { FleetUpdateDialog } from './FleetUpdates';

import type { Node } from '@/api/types';

function server(i: number, extra: Partial<Node> = {}): Node {
  return {
    id: `id-${i}`,
    name: `Server ${String(i).padStart(2, '0')}`,
    hostname: `s${i}.example.net`,
    engine: 'telemt',
    status: 'online',
    online: true,
    telemt_version: '3.5.7',
    tproxy_version: '',
    ...extra,
  } as Node;
}

let started: string[][] = [];

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function renderDialog(nodes: Node[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const onOpenChange = vi.fn();
  render(
    <QueryClientProvider client={qc}>
      <FleetUpdateDialog open onOpenChange={onOpenChange} nodes={nodes} />
    </QueryClientProvider>,
  );
  return { onOpenChange };
}

const rows = () => screen.getAllByTestId('fleet-server');

describe('FleetUpdateDialog', () => {
  beforeEach(() => {
    setLang('en');
    started = [];
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input), 'http://panel.test');
      if (url.pathname === '/api/v1/fleet/updates' && init?.method === 'POST') {
        started.push((JSON.parse(String(init.body)) as { node_ids: string[] }).node_ids);
        return json({ id: 'r1' }, 202);
      }
      if (url.pathname === '/api/v1/fleet/updates') return json({ items: [], version: '3.5.9' });
      return json({ error: { code: 'not_found', message: 'nope' } }, 404);
    }) as typeof fetch;
  });

  it('names the target version and lists only telemt servers with what each would get', async () => {
    renderDialog([
      server(1),
      server(2, { telemt_version: '3.5.9' }),
      server(3, { status: 'offline', online: false }),
      server(4, { engine: 'tproxy', telemt_version: '' }),
    ]);

    expect(await screen.findByRole('heading', { name: 'Update telemt to 3.5.9' })).toBeInTheDocument();
    expect(rows()).toHaveLength(3);
    expect(within(rows()[0]).getByText('3.5.7 → 3.5.9')).toBeInTheDocument();
    expect(within(rows()[1]).getByText('already 3.5.9')).toBeInTheDocument();
    expect(within(rows()[2]).getByText('not responding')).toBeInTheDocument();
    expect(within(rows()[1]).getByRole('checkbox')).toHaveAttribute('aria-disabled', 'true');
  });

  it('selects the outdated servers that can be updated and counts them', async () => {
    renderDialog([server(1), server(2, { telemt_version: '3.5.9' }), server(3, { status: 'offline', online: false }), server(4)]);
    await screen.findByRole('heading', { name: 'Update telemt to 3.5.9' });

    await userEvent.click(screen.getByRole('button', { name: 'Select outdated' }));

    expect(screen.getByText('2 of 4 selected, in list order')).toBeInTheDocument();
    expect(within(rows()[0]).getByText('1')).toBeInTheDocument();
    expect(within(rows()[3]).getByText('2')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Update 2 servers' })).toBeEnabled();
  });

  it('finds a server among many and keeps the list order when starting', async () => {
    const many = Array.from({ length: 60 }, (_, i) => server(i + 1));
    const { onOpenChange } = renderDialog(many);
    await screen.findByRole('heading', { name: 'Update telemt to 3.5.9' });
    expect(rows()).toHaveLength(60);

    await userEvent.type(screen.getByRole('searchbox', { name: 'Find a server' }), 'Server 4');
    expect(rows()).toHaveLength(10);
    await userEvent.click(within(rows()[3]).getByRole('checkbox'));
    await userEvent.clear(screen.getByRole('searchbox', { name: 'Find a server' }));
    await userEvent.type(screen.getByRole('searchbox', { name: 'Find a server' }), 's7.example');
    await userEvent.click(within(rows()[0]).getByRole('checkbox'));

    expect(screen.getByText('2 of 60 selected, in list order')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Update 2 servers' }));

    await waitFor(() => expect(started).toEqual([['id-7', 'id-43']]));
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it('starts nothing until a server is chosen', async () => {
    renderDialog([server(1)]);
    await screen.findByRole('heading', { name: 'Update telemt to 3.5.9' });
    expect(screen.getByRole('button', { name: 'Update 0 servers' })).toBeDisabled();
  });
});
