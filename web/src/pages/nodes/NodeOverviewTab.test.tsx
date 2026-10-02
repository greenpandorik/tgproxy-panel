import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { useNodeHealth } from '@/api/nodes';

import { NodeOverviewTab } from './NodeOverviewTab';

import type * as Probes from './probes';
import type { Node, NodeHealth } from '@/api/types';

const idle = { data: undefined, isLoading: false, isError: false, refetch: vi.fn() };
const mutation = { mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false, isError: false, data: undefined };

vi.mock('@/auth/AuthProvider', () => ({ useAuth: () => ({ isWriter: true }) }));
vi.mock('@/api/dashboard', () => ({ useAlerts: () => ({ data: { items: [] } }) }));
vi.mock('@/api/monitoring', () => ({ useNodeSeries: () => ({ ...idle, data: { points: [] } }) }));
vi.mock('@/api/nodes', () => ({
  useNodeHealth: vi.fn(),
  useNodeStats: () => idle,
  useRunNodeCheck: () => mutation,
  useNode: () => ({ data: undefined }),
  nodeKeys: { health: (id: string) => ['nodes', id, 'health'] },
}));
vi.mock('@/api/web', () => ({
  useNodeDiagnostics: () => ({ ...idle, data: { items: [] } }),
  useNodeWebCarriers: () => idle,
  useRunWebDiagnostics: () => mutation,
}));
vi.mock('./probes', async (importOriginal) => ({
  ...(await importOriginal<typeof Probes>()),
  useNodeProbes: () => ({ ...idle, data: { items: [], expected_locations: [], observedAt: Date.now() } }),
}));

const node = {
  id: 'n1',
  engine: 'telemt',
  online: true,
  status: 'online',
  last_seen_at: new Date().toISOString(),
  people_online: 6,
  connections: 41,
  last_check: null,
  telemt_capabilities: { Web: true },
} as unknown as Node;

function withHealth(over: Partial<NodeHealth>) {
  vi.mocked(useNodeHealth).mockReturnValue({
    ...idle,
    data: {
      relay_active: true,
      mtproxy_active: true,
      caddy_active: true,
      healthz: true,
      readyz: true,
      uptime_seconds: 3600,
      mem_used_percent: 38,
      disk_used_percent: 41,
      cpu_utilisation_percent: 24,
      dc_data_available: true,
      upstream_healthy: true,
      dcs: [1, 2, 3, 4, 5].map((dc) => ({ dc, latency_ms: 40 + dc, known: true, ip_preference: 'ipv4' })),
      ...over,
    },
  } as unknown as ReturnType<typeof useNodeHealth>);
}

const row = (name: RegExp) => screen.getByRole('button', { name });

describe('NodeOverviewTab', () => {
  beforeEach(() => setLang('ru'));

  it('keeps the detail rows closed and says how each one is', () => {
    withHealth({});
    render(
      <MemoryRouter>
        <NodeOverviewTab node={node} />
      </MemoryRouter>,
    );
    expect(row(/Датацентры Telegram/)).toHaveAttribute('aria-expanded', 'false');
    expect(row(/Датацентры Telegram/)).toHaveTextContent('все 5 доступны');
    expect(row(/Службы/)).toHaveAttribute('aria-expanded', 'false');
    expect(row(/Службы/)).toHaveTextContent('telemt, caddy, агент работают');
    expect(row(/Проверки сервера/)).toHaveTextContent('ещё не проводились');
    expect(row(/WEB-транспорт/)).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByRole('button', { name: 'Проверить сейчас' })).toBeInTheDocument();
  });

  it('opens the row that has a problem', () => {
    withHealth({ caddy_active: false });
    render(
      <MemoryRouter>
        <NodeOverviewTab node={node} />
      </MemoryRouter>,
    );
    expect(row(/Службы/)).toHaveAttribute('aria-expanded', 'true');
    expect(row(/Службы/)).toHaveTextContent('не работает: caddy');
    expect(row(/Датацентры Telegram/)).toHaveAttribute('aria-expanded', 'false');
  });

  it('opens the checks for a link that used to lead to the Проверки tab', () => {
    withHealth({});
    render(
      <MemoryRouter>
        <NodeOverviewTab node={node} focus="checks" />
      </MemoryRouter>,
    );
    expect(row(/Проверки сервера/)).toHaveAttribute('aria-expanded', 'true');
  });
});
