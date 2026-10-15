import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { KeyPeopleSection } from './KeyPeopleSection';
import { nodeShares } from './presence';

import type { KeyPresence } from '@/api/types';

function json(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } }));
}

function stubPresence(presence: KeyPresence) {
  const fetchMock = vi.fn((input: RequestInfo | URL) =>
    String(input).includes('/keys/k-1/presence') ? json(presence) : json({}),
  );
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

function renderSection(range: '24h' | '7d' = '24h') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <KeyPeopleSection keyId="k-1" range={range} />
    </QueryClientProvider>,
  );
}

const EMPTY_POINTS = Array.from({ length: 96 }, (_, i) => ({
  t: new Date(Date.UTC(2026, 9, 14, 12, 0) + (i + 1) * 900_000).toISOString(),
  people: null,
  connections: null,
}));

describe('KeyPeopleSection', () => {
  beforeEach(() => setLang('ru'));

  it('shows people now, over 15 minutes, the connections and each server with its share', async () => {
    const fetchMock = stubPresence({
      shared: true,
      range: '7d',
      step: 6300,
      now: { people: 12_345, people_15m: 15_002, connections: 40_100 },
      nodes: [
        { node_id: 'n-ams', node_name: 'Amsterdam', people: 7_500, connections: 25_000 },
        { node_id: 'n-fra', node_name: 'Frankfurt', people: 2_500, connections: 15_100 },
        { node_id: 'n-idle', node_name: 'Helsinki', people: 0, connections: 0 },
      ],
      points: EMPTY_POINTS,
    });
    renderSection('7d');

    expect(await screen.findByTestId('key-people-now')).toHaveTextContent('≈ 12 345');
    expect(screen.getByText('за 15 минут: ≈ 15 002')).toBeInTheDocument();
    expect(screen.getByText('соединений: 40 100')).toBeInTheDocument();
    expect(screen.getByText('Люди онлайн за 7 дней')).toBeInTheDocument();
    expect(String(fetchMock.mock.calls[0][0])).toContain('/api/v1/keys/k-1/presence?range=7d');

    const servers = within(screen.getByRole('list', { name: 'По серверам' })).getAllByRole('listitem');
    expect(servers.map((li) => li.textContent)).toEqual([
      'Amsterdam≈ 7 500 · 75 %',
      'Frankfurt≈ 2 500 · 25 %',
      'Helsinki0 · 0 %',
    ]);
    expect(screen.getByText('Для графика пока мало данных.')).toBeInTheDocument();
  });

  it('writes zero without the approximation sign and drops the shares when nobody is on', async () => {
    stubPresence({
      shared: true,
      range: '24h',
      step: 900,
      now: { people: 0, people_15m: 0, connections: 0 },
      nodes: [{ node_id: 'n-ams', node_name: 'Amsterdam', people: 0, connections: 0 }],
      points: EMPTY_POINTS,
    });
    renderSection();

    expect(await screen.findByTestId('key-people-now')).toHaveTextContent(/^0$/);
    expect(screen.getByText('за 15 минут: 0')).toBeInTheDocument();
    expect(screen.getByText('соединений: 0')).toBeInTheDocument();
    const servers = within(screen.getByRole('list', { name: 'По серверам' })).getAllByRole('listitem');
    expect(servers.map((li) => li.textContent)).toEqual(['Amsterdam0']);
    expect(screen.getByText('Люди онлайн за 24 часа')).toBeInTheDocument();
  });

  it('says how people are counted', async () => {
    stubPresence({
      shared: true,
      range: '24h',
      step: 900,
      now: { people: 3, people_15m: 4, connections: 9 },
      nodes: [],
      points: [],
    });
    renderSection();

    expect(await screen.findByText(/по разным IP-адресам на всех серверах/)).toBeInTheDocument();
    expect(screen.queryByRole('list', { name: 'По серверам' })).toBeNull();
  });
});

describe('nodeShares', () => {
  it('splits the people the servers count and has no share when they count nobody', () => {
    const node = (people: number) => ({ node_id: String(people), node_name: '', people, connections: 0 });
    expect(nodeShares([node(3), node(1)])).toEqual([0.75, 0.25]);
    expect(nodeShares([node(0), node(0)])).toEqual([null, null]);
  });
});
