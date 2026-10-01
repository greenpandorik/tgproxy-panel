import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AuthProvider } from '@/auth/AuthProvider';

import { UserDialog } from './UserDialog';

import type { AccessKey, Node } from '@/api/types';

const NODE = {
  id: 'n-ams',
  name: 'Amsterdam',
  hostname: 'ams1.proxy-demo.net',
  engine: 'telemt',
  status: 'online',
  online: true,
  max_profiles: 128,
  profile_count: 3,
} as Node;

const MARIA = {
  id: 'u-1',
  label: 'Мария С.',
  type: 'PERSONAL',
  owner_label: '@maria',
  status: 'active',
  carrier_mode: 'https',
  limits: {},
  telemt_limits: {},
  expires_at: null,
  revoked_at: null,
  note: '',
  created_at: '2026-09-01T00:00:00Z',
  nodes: [{ node_id: 'n-ams', node_name: 'Amsterdam', hostname: 'ams1.proxy-demo.net' }],
  client_support: { desktop: 'stable', android: 'experimental', ios: 'planned' },
  subscription_active: true,
  traffic_30d: 0,
  state: 'active',
  disabled_at: null,
  last_seen_at: null,
  sub_slug: null,
  subscription_url: 'https://panel.test/s/abc',
  subscription_short_url: null,
  subscription_legacy: false,
  live: { online: true, connections: 6, devices: 2, devices_15m: 3, ips: 2 },
} as unknown as AccessKey;

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function mockApi(patch: () => Promise<Response> = () => json(MARIA)) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes('/auth/me')) return json({ id: 'a1', username: 'root', role: 'owner', features: { totp: false } });
      if (url.endsWith('/api/v1/keys/u-1') && init?.method === 'PATCH') return patch();
      if (url.endsWith('/api/v1/keys/u-1')) return json(MARIA);
      if (url.includes('/api/v1/keys/u-1/links')) return json({ items: [], client_support: MARIA.client_support });
      if (url.includes('/api/v1/nodes')) return json({ items: [NODE], total: 1 });
      return json({ items: [], total: 0 });
    }),
  );
}

function renderDialog() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AuthProvider>
          <UserDialog open onOpenChange={() => {}} keyId="u-1" />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('UserDialog tabs', () => {
  beforeEach(() => {
    setLang('ru');
    window.localStorage.clear();
    document.cookie = 'tgwp_csrf=test-csrf';
  });

  it('keeps every part of the user one tab away', async () => {
    mockApi();
    renderDialog();

    expect(await screen.findByLabelText('Имя')).toHaveValue('Мария С.');
    expect(screen.getByText('В сети, ≈ 2 устр.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Прямые ссылки на прокси/ })).toBeInTheDocument();
    expect(screen.getByLabelText('Срок доступа')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('tab', { name: 'Доступ и серверы' }));
    expect(await screen.findByText('Тип доступа')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: /Amsterdam/ })).toBeChecked();

    await userEvent.click(screen.getByRole('tab', { name: 'Лимиты' }));
    expect(await screen.findByLabelText('Квота трафика, ГБ')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('tab', { name: 'Статистика' }));
    expect(await screen.findByText('Трафик и соединения')).toBeInTheDocument();
  });

  it('allows saving only after something changed', async () => {
    mockApi();
    renderDialog();

    const name = await screen.findByLabelText('Имя');
    const save = screen.getByRole('button', { name: 'Сохранить' });
    expect(save).toBeDisabled();

    await userEvent.type(name, ' Петрова');
    expect(save).toBeEnabled();
    expect(screen.getByText('Есть несохранённые изменения')).toBeInTheDocument();

    await userEvent.clear(name);
    await userEvent.type(name, 'Мария С.');
    await waitFor(() => expect(save).toBeDisabled());
  });

  it('takes you to the tab of a field the server rejected and marks it', async () => {
    mockApi(() => json({ error: { code: 'validation', message: 'invalid', fields: { telemt_limits: 'quota is too large' } } }, 422));
    renderDialog();

    await userEvent.type(await screen.findByLabelText('Имя'), ' Петрова');
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    const limits = await screen.findByRole('tab', { name: /Лимиты/ });
    await waitFor(() => expect(limits).toHaveAttribute('aria-selected', 'true'));
    expect(limits).toHaveTextContent('есть ошибки');
    expect(screen.getByText('quota is too large')).toBeInTheDocument();
  });

  it('brings back the tab that holds a field left empty', async () => {
    mockApi();
    renderDialog();

    await userEvent.clear(await screen.findByLabelText('Имя'));
    await userEvent.click(screen.getByRole('tab', { name: 'Статистика' }));
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    await waitFor(() => expect(screen.getByRole('tab', { name: /Главное/ })).toHaveAttribute('aria-selected', 'true'));
    expect(screen.getByText('Обязательное поле')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText('Имя')).toHaveFocus());
  });
});
