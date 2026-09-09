import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AuthProvider } from '@/auth/AuthProvider';

import { Sidebar } from './Sidebar';

function node(over: Record<string, unknown>) {
  return {
    id: 'n-1',
    name: 'Amsterdam',
    hostname: 'ams1.proxy-demo.net',
    public_ip: '185.10.20.30',
    acme_email: 'a@b.co',
    status: 'online',
    online: true,
    tproxy_version: '52a5feb',
    agent_version: '0.3.0',
    max_profiles: 128,
    profile_count: 4,
    dirty: false,
    last_seen_at: null,
    last_apply_at: null,
    created_at: '2026-01-01T00:00:00Z',
    last_check: null,
    ...over,
  };
}

const NODES = {
  items: [
    node({ status: 'online', online: false }),
    node({ id: 'n-2', name: 'Helsinki', hostname: 'hel1.proxy-demo.net', status: 'offline', online: true }),
  ],
  total: 2,
  page: 1,
  per_page: 50,
};

const SUMMARY = {
  nodes: { pending: 0, online: 0, offline: 0, degraded: 0, total: 0 },
  keys: { pending: 0, active: 3, revoked: 0, total: 3 },
};

function json(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } }));
}

function renderSidebar() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <AuthProvider>
          <Sidebar />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Sidebar navigation', () => {
  beforeEach(() => {
    setLang('ru');
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes('/api/v1/nodes')) return json(NODES);
        if (url.includes('/dashboard/summary')) return json(SUMMARY);
        if (url.includes('/auth/me')) return json({ id: 'u1', username: 'root', role: 'owner', features: { totp: false } });
        return json({});
      }),
    );
  });

  it('links to servers without duplicating dashboard counters', async () => {
    renderSidebar();
    expect(await screen.findByRole('link', { name: 'Серверы' })).toHaveAttribute('href', '/nodes');
    expect(screen.queryByText('1/2')).toBeNull();
  });
});
