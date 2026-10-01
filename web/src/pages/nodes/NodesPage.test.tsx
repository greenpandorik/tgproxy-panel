import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { useFleetRollouts, useNodes } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';

import { NodesPage } from './NodesPage';

import type * as NodesApi from '@/api/nodes';
import type { Node, NodeHealth } from '@/api/types';

vi.mock('@/auth/AuthProvider', () => ({ useAuth: vi.fn() }));
vi.mock('@/api/nodes', async (importOriginal) => ({
  ...(await importOriginal<typeof NodesApi>()),
  useNodes: vi.fn(),
  useFleetRollouts: vi.fn(),
}));
vi.mock('./CreateNodeDialog', () => ({ CreateNodeDialog: () => null }));

function wrap(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>
  );
}

function health(cpu: number, mem: number, extra: Partial<NodeHealth> = {}): NodeHealth {
  return {
    relay_active: true,
    mtproxy_active: true,
    caddy_active: true,
    healthz: true,
    readyz: true,
    tproxy_version: '',
    agent_version: '1',
    uptime_seconds: 10,
    cpu_percent: cpu,
    cpu_utilisation_percent: cpu,
    mem_used_percent: mem,
    disk_used_percent: 5,
    profile_count: 0,
    ...extra,
  };
}

function node(name: string, status: Node['status'], h?: NodeHealth): Node {
  return {
    id: `00000000-0000-4000-8000-0000000000${name.charCodeAt(1).toString(16)}`,
    name,
    hostname: `${name}.test`,
    public_ip: '',
    acme_email: 'a@b.co',
    status,
    online: status !== 'offline',
    engine: 'telemt',
    tls_domain: `${name}.test`,
    tls_domains: [],
    classic_port: 8443,
    ad_tag: '',
    telemt_version: '3.5.5',
    tproxy_version: '',
    agent_version: '1',
    max_profiles: 0,
    profile_count: 0,
    dirty: false,
    last_seen_at: null,
    last_apply_at: null,
    created_at: '2026-09-06T00:00:00Z',
    health: h,
    last_check: null,
  };
}

function rowOf(name: string): HTMLElement {
  const row = screen
    .getAllByText(name)
    .map((el) => el.closest('tr'))
    .find((tr): tr is HTMLTableRowElement => tr !== null);
  if (!row) throw new Error(`no table row for ${name}`);
  return row;
}

beforeEach(() => {
  vi.mocked(useFleetRollouts).mockReturnValue({ data: { items: [], version: '3.5.9' } } as unknown as ReturnType<
    typeof useFleetRollouts
  >);
});

describe('NodesPage load columns', () => {
  beforeEach(() => {
    vi.mocked(useAuth).mockReturnValue({ isWriter: false } as unknown as ReturnType<typeof useAuth>);
    setLang('en');
  });

  it('leaves CPU blank when the node reported no utilisation, rather than showing its load average', () => {
    vi.mocked(useNodes).mockReturnValue({
      data: {
        items: [node('n1', 'online', health(42, 85, { cpu_utilisation_percent: undefined }))],
        total: 1,
      },
      isLoading: false,
    } as unknown as ReturnType<typeof useNodes>);

    render(wrap(<NodesPage />));

    const bars = within(rowOf('n1')).getAllByTestId('load-bar');
    expect(bars).toHaveLength(1);
    expect(bars[0]).toHaveTextContent('85%');
    expect(within(rowOf('n1')).queryByText('42%')).toBeNull();
  });

  it('draws CPU and RAM bars from the last heartbeat, toned at 80% and 95%', () => {
    vi.mocked(useNodes).mockReturnValue({
      data: { items: [node('n1', 'online', health(42, 85)), node('n2', 'degraded', health(97, 10))], total: 2 },
      isLoading: false,
    } as unknown as ReturnType<typeof useNodes>);

    render(wrap(<NodesPage />));

    expect(screen.getByRole('columnheader', { name: 'CPU' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'RAM' })).toBeInTheDocument();

    const n1 = within(rowOf('n1')).getAllByTestId('load-bar');
    expect(n1).toHaveLength(2);
    expect(n1[0]).toHaveAttribute('data-tone', 'ok');
    expect(n1[0]).toHaveTextContent('42%');
    expect(n1[0].querySelector('.bg-brand-primary')).not.toBeNull();
    expect(n1[1]).toHaveAttribute('data-tone', 'warn');
    expect(n1[1]).toHaveTextContent('85%');
    expect(n1[1].querySelector('.bg-warn')).not.toBeNull();

    const n2 = within(rowOf('n2')).getAllByTestId('load-bar');
    expect(n2[0]).toHaveAttribute('data-tone', 'danger');
    expect(n2[0]).toHaveTextContent('97%');
    expect(n2[0].querySelector('.bg-err')).not.toBeNull();
    expect(n2[1]).toHaveAttribute('data-tone', 'ok');
  });

  it('prints a dash instead of a stale or missing figure', () => {
    vi.mocked(useNodes).mockReturnValue({
      data: { items: [node('n3', 'offline', health(50, 50)), node('n4', 'online')], total: 2 },
      isLoading: false,
    } as unknown as ReturnType<typeof useNodes>);

    render(wrap(<NodesPage />));

    // Offline: the last health describes a moment the panel cannot vouch for.
    expect(within(rowOf('n3')).queryAllByTestId('load-bar')).toHaveLength(0);
    expect(screen.queryByText('50%')).toBeNull();
    // Never reported: nothing to draw.
    expect(within(rowOf('n4')).queryAllByTestId('load-bar')).toHaveLength(0);
  });
});

describe('NodesPage online column', () => {
  beforeEach(() => {
    vi.mocked(useAuth).mockReturnValue({ isWriter: false } as unknown as ReturnType<typeof useAuth>);
    setLang('en');
  });

  it('shows people online with the connections under them, and a dash without a fresh count', () => {
    vi.mocked(useNodes).mockReturnValue({
      data: {
        items: [
          { ...node('n1', 'online', health(1, 1)), people_online: 3, connections: 17 },
          { ...node('n2', 'online', health(1, 1)), people_online: null, connections: 4 },
          node('n3', 'offline', health(1, 1)),
        ],
        total: 3,
      },
      isLoading: false,
    } as unknown as ReturnType<typeof useNodes>);

    render(wrap(<NodesPage />));

    expect(screen.getByRole('columnheader', { name: 'People online' })).toBeInTheDocument();
    const online = (name: string) => within(rowOf(name)).getAllByRole('cell')[1];
    expect(online('n1')).toHaveTextContent('≈ 3');
    expect(online('n1')).toHaveTextContent('connections: 17');
    expect(online('n2')).toHaveTextContent('—');
    expect(online('n2')).toHaveTextContent('connections: 4');
    expect(online('n3')).toHaveTextContent(/^—$/);
  });
});

function wrapWithDetailRoute(nodeEl: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={nodeEl} />
          <Route path="/nodes/:id" element={<div>node detail page</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe('NodesPage rows', () => {
  beforeEach(() => setLang('en'));

  it('opens the node from anywhere in its row, and marks the row as something to click', async () => {
    vi.mocked(useAuth).mockReturnValue({ isWriter: false } as unknown as ReturnType<typeof useAuth>);
    vi.mocked(useNodes).mockReturnValue({
      data: { items: [node('n1', 'online', health(42, 85))], total: 1 },
      isLoading: false,
    } as unknown as ReturnType<typeof useNodes>);

    render(wrapWithDetailRoute(<NodesPage />));

    expect(rowOf('n1').className).toContain('cursor-pointer');
    expect(within(rowOf('n1')).queryByRole('button')).toBeNull();
    await userEvent.click(within(rowOf('n1')).getAllByTestId('load-bar')[0]);
    expect(await screen.findByText('node detail page')).toBeInTheDocument();
  });

  it('gives a writer a grip to reorder that does not open the server', async () => {
    vi.mocked(useAuth).mockReturnValue({ isWriter: true } as unknown as ReturnType<typeof useAuth>);
    vi.mocked(useNodes).mockReturnValue({
      data: { items: [node('n1', 'online', health(42, 85)), node('n2', 'online', health(1, 1))], total: 2 },
      isLoading: false,
    } as unknown as ReturnType<typeof useNodes>);

    render(wrapWithDetailRoute(<NodesPage />));

    const grip = within(rowOf('n1')).getByRole('button', { name: 'Move n1' });
    await userEvent.click(grip);
    expect(screen.queryByText('node detail page')).toBeNull();
    expect(screen.getByText(/Drag a row by its handle/)).toBeInTheDocument();
  });

  it('shows a viewer no grips', () => {
    vi.mocked(useAuth).mockReturnValue({ isWriter: false } as unknown as ReturnType<typeof useAuth>);
    vi.mocked(useNodes).mockReturnValue({
      data: { items: [node('n1', 'online', health(42, 85)), node('n2', 'online', health(1, 1))], total: 2 },
      isLoading: false,
    } as unknown as ReturnType<typeof useNodes>);

    render(wrap(<NodesPage />));

    expect(screen.queryByRole('button', { name: /^Move/ })).toBeNull();
  });
});

describe('NodesPage fleet update', () => {
  beforeEach(() => {
    setLang('en');
    vi.mocked(useAuth).mockReturnValue({ isWriter: true } as unknown as ReturnType<typeof useAuth>);
  });

  it('does not offer a telemt update when no server runs telemt', () => {
    vi.mocked(useNodes).mockReturnValue({
      data: { items: [{ ...node('n1', 'online'), engine: 'tproxy' }], total: 1 },
      isLoading: false,
    } as unknown as ReturnType<typeof useNodes>);

    render(wrap(<NodesPage />));

    expect(screen.queryByRole('button', { name: 'Update telemt…' })).toBeNull();
  });

  it('offers it once a telemt server is in the list', () => {
    vi.mocked(useNodes).mockReturnValue({
      data: { items: [node('n1', 'online')], total: 1 },
      isLoading: false,
    } as unknown as ReturnType<typeof useNodes>);

    render(wrap(<NodesPage />));

    expect(screen.getByRole('button', { name: 'Update telemt…' })).toBeInTheDocument();
  });
});
