import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { KeyStatsSection } from './KeyStatsSection';

import type { KeyStats } from '@/api/types';

function json(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } }));
}

function stubStats(stats: KeyStats) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => (String(input).includes('/keys/k-1/stats') ? json(stats) : json({}))),
  );
}

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <KeyStatsSection keyId="k-1" hasTelemtNode range="24h" />
    </QueryClientProvider>,
  );
}

describe('KeyStatsSection', () => {
  beforeEach(() => setLang('ru'));

  it('says the chart has too little data when a node has sent a single sample', async () => {
    stubStats({
      totals: { connections_now: 5, octets_delta: 0 },
      nodes: [
        {
          node_id: 'n-1',
          node_name: 'Amsterdam',
          points: [{ t: '2026-09-30T18:00:00Z', connections: 5, total_octets: 1000 }],
        },
      ],
    });
    renderSection();

    expect(await screen.findByText('Для графика пока мало данных.')).toBeInTheDocument();
    expect(screen.getByText('Трафик за 24 часа')).toBeInTheDocument();
  });

  it('draws no placeholder once there are enough samples for a line', async () => {
    stubStats({
      totals: { connections_now: 2, octets_delta: 3000 },
      nodes: [
        {
          node_id: 'n-1',
          node_name: 'Amsterdam',
          points: [
            { t: '2026-09-30T18:00:00Z', connections: 2, total_octets: 1000 },
            { t: '2026-09-30T18:05:00Z', connections: 2, total_octets: 2000 },
            { t: '2026-09-30T18:10:00Z', connections: 2, total_octets: 4000 },
          ],
        },
      ],
    });
    renderSection();

    expect(await screen.findByText('Amsterdam')).toBeInTheDocument();
    expect(screen.queryByText('Для графика пока мало данных.')).toBeNull();
  });
});
