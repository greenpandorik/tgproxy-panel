import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { useNodes } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';

import { NodesPage } from './NodesPage';

import type * as NodesApi from '@/api/nodes';
import type { Node, NodeHealth } from '@/api/types';

vi.mock('@/auth/AuthProvider', () => ({ useAuth: vi.fn() }));
vi.mock('@/api/nodes', async (importOriginal) => ({
  ...(await importOriginal<typeof NodesApi>()),
  useNodes: vi.fn(),
}));
// The create dialog is its own component with its own tests; the list under
// test only needs it mounted closed.
vi.mock('./CreateNodeDialog', () => ({ CreateNodeDialog: () => null }));

function wrap(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>
  );
}

function health(cpu: number, mem: number): NodeHealth {
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
    mem_used_percent: mem,
    disk_used_percent: 5,
    profile_count: 0,
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
    classic_port: 8443,
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

// The name appears twice - once in the wide table, once in the narrow card
// list (jsdom applies no CSS, so both layouts are in the tree); the table row
// is the one with a <tr> above it.
function rowOf(name: string): HTMLElement {
  const row = screen
    .getAllByText(name)
    .map((el) => el.closest('tr'))
    .find((tr): tr is HTMLTableRowElement => tr !== null);
  if (!row) throw new Error(`no table row for ${name}`);
  return row;
}

describe('NodesPage load columns', () => {
  beforeEach(() => {
    vi.mocked(useAuth).mockReturnValue({ isWriter: false } as unknown as ReturnType<typeof useAuth>);
    setLang('en');
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

    // The narrow layout carries the same figures as text.
    expect(screen.getAllByText('42%').length).toBeGreaterThanOrEqual(2);
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
