import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, useLocation } from 'react-router-dom';
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
    live: { online: false, connections: 0, devices: 0, devices_15m: 0, ips: 0 },
  } as AccessKey;
}

function json(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } }));
}

const AMS = { id: 'n-ams', name: 'Amsterdam', hostname: 'ams1.proxy-demo.net', engine: 'telemt' };

function mockApi(items: AccessKey[], total = items.length) {
  const calls: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      calls.push(url);
      if (url.includes('/auth/me')) return json({ id: 'u1', username: 'root', role: 'owner', features: { totp: false } });
      if (url.includes('/keys/summary')) {
        return json({ total, active: total - 3, expiring: 1, expired: 1, disabled: 2, revoked: 0 });
      }
      if (url.includes('/api/v1/keys?')) return json({ items, total, page: 1, per_page: 25 });
      const one = items.find((k) => url.endsWith(`/api/v1/keys/${k.id}`));
      if (one) return json(one);
      if (url.includes('/api/v1/nodes')) return json({ items: [AMS], total: 1 });
      return json({ items: [], total: 0 });
    }),
  );
  return {
    lists: () => calls.filter((u) => u.includes('/api/v1/keys?')).map((u) => new URL(u, 'http://panel.test').searchParams),
  };
}

function LocationProbe() {
  const location = useLocation();
  return <output data-testid="location">{location.search}</output>;
}

const where = () => new URLSearchParams(screen.getByTestId('location').textContent ?? '');

function renderPage(entry = '/users') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[entry]}>
        <AuthProvider>
          <UsersPage />
          <LocationProbe />
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

  it('says which rows are shown and draws no pages when everyone fits', async () => {
    const api = mockApi([user('u-1', 'Ольга К.'), user('u-2', 'Мария С.')]);
    renderPage();

    expect(await screen.findByText('Показаны 1–2 из 2')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Следующая страница' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Сбросить фильтры' })).toBeNull();
    expect(api.lists().at(-1)?.get('per_page')).toBe('25');
  });

  it('remembers the chosen number of rows', async () => {
    const api = mockApi([user('u-1', 'Ольга К.')], 120);
    const view = renderPage('/users?page=3');

    await userEvent.selectOptions(await screen.findByLabelText('Строк на странице'), '50');
    await waitFor(() => expect(api.lists().at(-1)?.get('per_page')).toBe('50'));
    expect(window.localStorage.getItem('tgwp-users-page-size')).toBe('50');
    expect(where().get('page')).toBe('2');

    view.unmount();
    renderPage();
    expect(await screen.findByLabelText('Строк на странице')).toHaveValue('50');
    await waitFor(() => expect(api.lists().at(-1)?.get('per_page')).toBe('50'));
  });

  it('keeps the page in the address and numbers the pages', async () => {
    const api = mockApi([user('u-1', 'Ольга К.')], 132);
    renderPage();

    const pages = await screen.findByRole('navigation', { name: 'Страницы' });
    expect(within(pages).getAllByRole('button').map((b) => b.textContent)).toEqual(['', '1', '2', '3', '6', '']);
    expect(within(pages).getByRole('button', { name: 'Страница 1' })).toHaveAttribute('aria-current', 'page');
    expect(within(pages).getByRole('button', { name: 'Предыдущая страница' })).toBeDisabled();

    await userEvent.click(within(pages).getByRole('button', { name: 'Страница 2' }));
    await waitFor(() => expect(where().get('page')).toBe('2'));
    await waitFor(() => expect(api.lists().at(-1)?.get('page')).toBe('2'));
    expect(screen.getByText('Показаны 26–50 из 132')).toBeInTheDocument();

    await userEvent.click(within(pages).getByRole('button', { name: 'Предыдущая страница' }));
    await waitFor(() => expect(where().has('page')).toBe(false));
  });

  it('goes back to the first page when a filter changes', async () => {
    mockApi([user('u-1', 'Ольга К.')], 132);
    renderPage('/users?page=4');

    await userEvent.click(await screen.findByRole('button', { name: /^Выключены/ }));
    await waitFor(() => expect(where().get('state')).toBe('disabled'));
    expect(where().has('page')).toBe(false);
    expect(screen.getByRole('button', { name: /^Выключены/ })).toHaveAttribute('aria-pressed', 'true');

    await userEvent.click(screen.getByRole('button', { name: 'Страница 2' }));
    await waitFor(() => expect(where().get('page')).toBe('2'));
    await userEvent.type(screen.getByRole('searchbox', { name: 'Поиск' }), 'оля');
    await waitFor(() => expect(where().get('q')).toBe('оля'));
    expect(where().has('page')).toBe(false);
  });

  it('filters by the server named in the address and clears it', async () => {
    const api = mockApi([user('u-1', 'Ольга К.')]);
    renderPage('/users?node=n-ams');

    expect(await screen.findByText('Сервер: Amsterdam')).toBeInTheDocument();
    expect(api.lists().at(-1)?.get('node')).toBe('n-ams');

    await userEvent.click(screen.getByRole('button', { name: 'Убрать фильтр «Сервер: Amsterdam»' }));
    await waitFor(() => expect(where().has('node')).toBe(false));
    await waitFor(() => expect(api.lists().at(-1)?.has('node')).toBe(false));
  });

  it('opens a user from anywhere on the row but not from the checkbox', async () => {
    mockApi([user('u-1', 'Ольга К.')]);
    renderPage();

    const row = (await screen.findAllByText('Ольга К.'))[0].closest('tr') as HTMLElement;
    await userEvent.click(within(row).getByRole('checkbox'));
    expect(where().has('user')).toBe(false);
    expect(within(row).getByRole('checkbox')).toBeChecked();

    await userEvent.click(within(row).getByText('Активен'));
    await waitFor(() => expect(where().get('user')).toBe('u-1'));
  });
});

describe('UsersPage activity', () => {
  beforeEach(() => {
    setLang('ru');
    window.localStorage.clear();
  });

  it('counts devices for someone online and keeps the connections as a detail', async () => {
    const online = user('u-1', 'Ольга К.');
    online.live = { online: true, connections: 6, devices: 2, devices_15m: 3, ips: 2 };
    const away = user('u-2', 'Мария С.');
    away.last_seen_at = '2026-09-30T10:00:00Z';
    mockApi([online, away]);
    renderPage();

    expect((await screen.findAllByText('В сети, ≈ 2 устр.')).length).toBeGreaterThan(0);
    expect(screen.getAllByText('6 соединений').length).toBeGreaterThan(0);
    expect(screen.getByText('Не в сети')).toBeInTheDocument();
  });
});
