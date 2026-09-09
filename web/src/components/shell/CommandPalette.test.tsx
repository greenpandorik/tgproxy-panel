import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { ThemeProvider } from '@/theme/ThemeProvider';

import { CommandPalette } from './CommandPalette';

function node(over: Record<string, unknown>) {
  return {
    id: 'n-ams',
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

// One clean node and two with unapplied changes, so "Применить всё" has something to confirm.
const NODES = {
  items: [
    node({}),
    node({ id: 'n-fra', name: 'Frankfurt', hostname: 'fra1.proxy-demo.net', dirty: true }),
    node({ id: 'n-hel', name: 'Helsinki', hostname: 'hel1.proxy-demo.net', dirty: true }),
  ],
  total: 3,
  page: 1,
  per_page: 50,
};

const KEYS = { items: [], total: 0, page: 1, per_page: 50 };

function json(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } }));
}

function LocationProbe() {
  const { pathname, search } = useLocation();
  return <div data-testid="location">{pathname + search}</div>;
}

function renderPalette() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onOpenChange = vi.fn();
  const view = render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <ThemeProvider>
          <LocationProbe />
          <CommandPalette open onOpenChange={onOpenChange} />
        </ThemeProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...view, onOpenChange };
}

describe('CommandPalette', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    setLang('ru');
    fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/apply')) return json({ queued: true });
      if (url.includes('/api/v1/nodes')) return json(NODES);
      if (url.includes('/api/v1/keys')) return json(KEYS);
      return json({});
    });
    vi.stubGlobal('fetch', fetchMock);
  });

  const applyCalls = () => fetchMock.mock.calls.filter(([url]) => String(url).includes('/apply')).length;

  it('renders the section, node and action groups', async () => {
    renderPalette();

    expect(await screen.findByText('Разделы')).toBeInTheDocument();
    expect(screen.getByText('Действия')).toBeInTheDocument();
    expect(screen.getByText('Обзор')).toBeInTheDocument();
    expect(screen.getByText('Создать ключ')).toBeInTheDocument();
    expect(screen.getByText('Применить всё')).toBeInTheDocument();

    // Nodes arrive from the cached list query, searchable by name and host.
    expect(await screen.findByText('Ноды')).toBeInTheDocument();
    expect(await screen.findByText('Amsterdam')).toBeInTheDocument();
    expect(screen.getByText('ams1.proxy-demo.net')).toBeInTheDocument();
  });

  it('navigates to a section when Enter is pressed on it', async () => {
    const user = userEvent.setup();
    const { onOpenChange } = renderPalette();

    const input = await screen.findByPlaceholderText('Поиск, команды…');
    await user.click(input);
    // First item is the dashboard (already the current route); step to Monitoring.
    await user.keyboard('{ArrowDown}{Enter}');

    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/monitoring'));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('deep-links the create-key action into the keys page', async () => {
    const user = userEvent.setup();
    renderPalette();

    await user.click(await screen.findByText('Создать ключ'));

    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/keys?create=1'));
  });

  it('confirms before applying to every dirty node', async () => {
    const user = userEvent.setup();
    renderPalette();

    await user.click(await screen.findByText('Применить всё'));

    // The palette closes and the confirm takes its place - still nothing sent.
    expect(await screen.findByText('Применить изменения на 2 нодах?')).toBeInTheDocument();
    expect(screen.getByText(/Активные сессии на этих нодах оборвутся/)).toBeInTheDocument();
    expect(applyCalls()).toBe(0);

    await user.click(screen.getByRole('button', { name: 'Применить всё' }));

    await waitFor(() => expect(applyCalls()).toBe(2));
  });

  it('sends nothing and reports when no node has pending changes', async () => {
    const user = userEvent.setup();
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/api/v1/nodes')) return json({ ...NODES, items: [NODES.items[0]] });
      if (url.includes('/api/v1/keys')) return json(KEYS);
      return json({});
    });
    renderPalette();

    await user.click(await screen.findByText('Применить всё'));

    await waitFor(() => expect(screen.queryByText(/Применить изменения на/)).not.toBeInTheDocument());
    expect(applyCalls()).toBe(0);
  });
});
