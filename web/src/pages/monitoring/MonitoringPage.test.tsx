import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { MonitoringPage } from './MonitoringPage';

const NODE = { node_id: 'n1', node_name: 'Amsterdam', hostname: 'ams1.example.net', status: 'online' };

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function renderPage(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <MonitoringPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('MonitoringPage', () => {
  beforeEach(() => {
    setLang('en');
    globalThis.fetch = vi.fn((input: RequestInfo | URL) => {
      const url = new URL(String(input), 'http://panel.test');
      if (url.pathname === '/api/v1/monitoring/overview') return json({ nodes: [NODE], series: { n1: [] } });
      if (url.pathname === '/api/v1/nodes') return json({ items: [] });
      return json({ error: { code: 'not_found', message: 'nope' } }, 404);
    }) as typeof fetch;
  });

  it('shows the fleet summary without a range that would not change it', async () => {
    renderPage('/monitoring');

    expect(await screen.findByText('Fleet health')).toBeInTheDocument();
    expect(screen.queryByRole('radiogroup', { name: 'Range' })).toBeNull();
  });

  it('offers the range next to the per-server charts', async () => {
    renderPage('/monitoring?section=nodes');

    expect(await screen.findByText('ams1.example.net')).toBeInTheDocument();
    expect(screen.getByRole('radiogroup', { name: 'Range' })).toBeInTheDocument();
  });

  it('puts people online in the summary, connections under them', async () => {
    globalThis.fetch = vi.fn((input: RequestInfo | URL) => {
      const url = new URL(String(input), 'http://panel.test');
      if (url.pathname === '/api/v1/monitoring/overview') {
        return json({
          nodes: [NODE],
          series: { n1: [] },
          fleet: { people_online: 7, people_online_15m: 9, connections: 33 },
        });
      }
      return json({ items: [] });
    }) as typeof fetch;
    renderPage('/monitoring');

    expect(await screen.findByText('≈ 7')).toBeInTheDocument();
    expect(screen.getByText('People online')).toBeInTheDocument();
    expect(screen.getByText('connections: 33')).toBeInTheDocument();
  });

  it('says the head count is missing rather than zero', async () => {
    renderPage('/monitoring');

    expect(await screen.findByText('People online')).toBeInTheDocument();
    expect(screen.queryByText('≈ 0')).toBeNull();
    expect(screen.getAllByText('Not available').length).toBeGreaterThan(0);
  });
});
