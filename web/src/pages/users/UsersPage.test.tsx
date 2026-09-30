import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AuthProvider } from '@/auth/AuthProvider';

import { UsersPage } from './UsersPage';

import type { AccessKey } from '@/api/types';

function user(id: string, label: string): AccessKey {
  return {
    id,
    label,
    type: 'PERSONAL',
    owner_label: '',
    status: 'active',
    carrier_mode: 'https',
    limits: {},
    telemt_limits: {},
    expires_at: null,
    revoked_at: null,
    note: '',
    created_at: '2026-09-01T00:00:00Z',
    nodes: [],
    client_support: { desktop: 'stable', android: 'experimental', ios: 'planned' },
    subscription_active: true,
    traffic_30d: 0,
    state: 'active',
    disabled_at: null,
    last_seen_at: null,
    sub_slug: null,
    subscription_url: null,
    subscription_short_url: null,
    subscription_legacy: false,
    live: { connections: 0, ips: 0 },
  } as AccessKey;
}

function json(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } }));
}

function mockApi(items: AccessKey[], total = items.length) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/auth/me')) return json({ id: 'u1', username: 'root', role: 'owner', features: { totp: false } });
      if (url.includes('/keys/summary')) {
        return json({ total, active: total, expiring: 0, expired: 0, disabled: 0, revoked: 0 });
      }
      if (url.includes('/api/v1/keys?')) return json({ items, total, page: 1, per_page: 50 });
      return json({ items: [], total: 0 });
    }),
  );
}

function renderPage(entry = '/users') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[entry]}>
        <AuthProvider>
          <UsersPage />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('UsersPage pagination', () => {
  beforeEach(() => {
    setLang('ru');
    window.localStorage.clear();
  });

  it('shows no pager when every user fits on one page', async () => {
    mockApi([user('u-1', 'Ольга К.'), user('u-2', 'Мария С.')]);
    renderPage();

    expect((await screen.findAllByText('Ольга К.')).length).toBeGreaterThan(0);
    expect(screen.queryByRole('button', { name: /Вперёд/ })).toBeNull();
    expect(screen.queryByText(/Страница 1 из 1/)).toBeNull();
    expect(screen.queryByRole('button', { name: 'Сбросить фильтры' })).toBeNull();
  });

  it('says how many users a filter found', async () => {
    mockApi([user('u-1', 'Ольга К.')]);
    renderPage('/users?state=active');

    expect(await screen.findByText('Найдено: 1')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Вперёд/ })).toBeNull();
    expect(screen.getByRole('button', { name: 'Сбросить фильтры' })).toBeInTheDocument();
  });

  it('pages through a long list', async () => {
    mockApi([user('u-1', 'Ольга К.')], 120);
    renderPage();

    expect(await screen.findByText('Страница 1 из 3 · найдено: 120')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Вперёд/ })).toBeEnabled();
    expect(screen.getByRole('button', { name: /Назад/ })).toBeDisabled();
  });
});
