import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { useAuth } from '@/auth/AuthProvider';

import { MonitoringPage } from './MonitoringPage';

import type { Node, NodeHealth } from '@/api/types';

vi.mock('@/auth/AuthProvider', () => ({ useAuth: vi.fn() }));

const DAY = 86_400_000;
const NODE = { node_id: 'n1', node_name: 'Amsterdam', hostname: 'ams1.example.net', status: 'online' };

function health(extra: Partial<NodeHealth> = {}): NodeHealth {
  return {
    relay_active: true,
    mtproxy_active: true,
    caddy_active: true,
    healthz: true,
    readyz: true,
    tproxy_version: '',
    agent_version: '1',
    uptime_seconds: 12 * 86400,
    cpu_percent: 24,
    cpu_utilisation_percent: 24,
    mem_used_percent: 38,
    disk_used_percent: 5,
    profile_count: 0,
    ...extra,
  };
}

function server(id: string, name: string, extra: Partial<Node> = {}): Node {
  return {
    id,
    name,
    hostname: `${name.toLowerCase()}.example.net`,
    status: 'online',
    online: true,
    engine: 'telemt',
    telemt_version: '3.5.9',
    tproxy_version: '',
    dirty: false,
    last_seen_at: new Date().toISOString(),
    health: health(),
    people_online: 6,
    connections: 60,
    ...extra,
  } as Node;
}

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function mockPanel({ overview, nodes }: { overview: unknown; nodes: Node[] }) {
  globalThis.fetch = vi.fn((input: RequestInfo | URL) => {
    const url = new URL(String(input), 'http://panel.test');
    if (url.pathname === '/api/v1/monitoring/overview') return json(overview);
    if (url.pathname === '/api/v1/nodes') return json({ items: nodes, total: nodes.length });
    if (url.pathname === '/api/v1/fleet/updates') return json({ items: [], version: '3.5.9' });
    return json({ error: { code: 'not_found', message: 'nope' } }, 404);
  }) as typeof fetch;
}

function renderPage(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
          <Route path="/settings" element={<div>settings page</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('MonitoringPage', () => {
  beforeEach(() => {
    setLang('en');
    vi.mocked(useAuth).mockReturnValue({ isWriter: false } as unknown as ReturnType<typeof useAuth>);
    mockPanel({ overview: { nodes: [NODE], series: { n1: [] } }, nodes: [server('n1', 'Amsterdam')] });
  });

  it('shows the fleet summary without a range that would not change it', async () => {
    renderPage('/monitoring');

    expect(await screen.findByRole('group', { name: 'Fleet health' })).toBeInTheDocument();
    expect(screen.queryByRole('radiogroup', { name: 'Range' })).toBeNull();
  });

  it('offers the range next to the per-server charts', async () => {
    renderPage('/monitoring?section=nodes');

    expect(await screen.findByText('ams1.example.net')).toBeInTheDocument();
    expect(screen.getByRole('radiogroup', { name: 'Range' })).toBeInTheDocument();
  });

  it('sends the old metrics export view to Settings', async () => {
    renderPage('/monitoring?section=integrations');
    expect(await screen.findByText('settings page')).toBeInTheDocument();
  });

  it('puts people online in the summary, connections under them', async () => {
    mockPanel({
      overview: { nodes: [NODE], series: { n1: [] }, fleet: { people_online: 7, people_online_15m: 9, connections: 33 } },
      nodes: [server('n1', 'Amsterdam')],
    });
    renderPage('/monitoring');

    await screen.findByText('≈ 7');
    const summary = screen.getByRole('group', { name: 'Fleet health' });
    expect(within(summary).getByText('≈ 7')).toBeInTheDocument();
    expect(within(summary).getByText('connections: 33')).toBeInTheDocument();
  });

  it('says the head count is missing rather than zero', async () => {
    renderPage('/monitoring');

    await screen.findByRole('heading', { name: 'Amsterdam' });
    const summary = screen.getByRole('group', { name: 'Fleet health' });
    expect(within(summary).queryByText('≈ 0')).toBeNull();
    expect(within(summary).getAllByText('Not available').length).toBeGreaterThan(0);
  });

  it('names the slowest route, the nearest certificate and the server that is down', async () => {
    const dcs = [
      { dc: 2, latency_ms: 90, known: true, ip_preference: 'ipv4' },
      { dc: 5, latency_ms: 188, known: true, ip_preference: 'ipv4' },
    ];
    mockPanel({
      overview: {
        nodes: [
          { ...NODE, cert_expires_at: new Date(Date.now() + 61.5 * DAY).toISOString() },
          {
            node_id: 'n2',
            node_name: 'Frankfurt',
            hostname: 'fra',
            status: 'online',
            cert_expires_at: new Date(Date.now() + 74.5 * DAY).toISOString(),
          },
          { node_id: 'n3', node_name: 'Helsinki', hostname: 'hel', status: 'offline' },
        ],
        series: {},
      },
      nodes: [
        server('n1', 'Amsterdam'),
        server('n2', 'Frankfurt', { health: health({ dc_data_available: true, dcs }) }),
        server('n3', 'Helsinki', { status: 'offline', online: false, people_online: null }),
      ],
    });
    renderPage('/monitoring');

    await screen.findByText('2 / 3');
    const summary = screen.getByRole('group', { name: 'Fleet health' });
    expect(within(summary).getByText('2 / 3')).toBeInTheDocument();
    expect(within(summary).getByText('Helsinki is not responding')).toBeInTheDocument();
    expect(within(summary).getByText('188 ms')).toBeInTheDocument();
    expect(within(summary).getByText('Frankfurt, DC5')).toBeInTheDocument();
    expect(within(summary).getByText('all fine')).toBeInTheDocument();
    expect(within(summary).getByText('nearest in 61 days')).toBeInTheDocument();
  });

  it('gives every server a card with what the panel knows about it', async () => {
    mockPanel({
      overview: {
        nodes: [
          NODE,
          {
            node_id: 'n3',
            node_name: 'Helsinki',
            hostname: 'hel',
            status: 'offline',
            cert_expires_at: new Date(Date.now() + 52.5 * DAY).toISOString(),
          },
        ],
        series: {},
      },
      nodes: [
        server('n1', 'Amsterdam', { dirty: true, dirty_since: new Date(Date.now() - 12 * 60_000).toISOString() }),
        server('n3', 'Helsinki', {
          status: 'offline',
          online: false,
          people_online: null,
          telemt_version: '3.5.7',
          last_seen_at: new Date(Date.now() - 2 * DAY).toISOString(),
        }),
      ],
    });
    renderPage('/monitoring');

    const live = (await screen.findByRole('heading', { name: 'Amsterdam' })).closest('li') as HTMLElement;
    expect(within(live).getByText('≈ 6')).toBeInTheDocument();
    expect(within(live).getByText('24%')).toBeInTheDocument();
    expect(within(live).getByText('38%')).toBeInTheDocument();
    expect(within(live).getByText('waiting 12m')).toBeInTheDocument();

    const down = screen.getByRole('heading', { name: 'Helsinki' }).closest('li') as HTMLElement;
    expect(within(down).getByText('Not responding for 2d — last known:')).toBeInTheDocument();
    expect(within(down).getByText('52 days')).toBeInTheDocument();
    expect(within(down).getByText('3.5.7, 3.5.9 available')).toBeInTheDocument();
    expect(within(down).queryByText('24%')).toBeNull();
  });
});
