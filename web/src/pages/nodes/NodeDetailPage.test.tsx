import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { useNode } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { ApiError } from '@/lib/api';

import { NodeDetailPage } from './NodeDetailPage';

import type * as NodesApi from '@/api/nodes';

vi.mock('@/auth/AuthProvider', () => ({ useAuth: vi.fn() }));
vi.mock('@/api/nodes', async (importOriginal) => ({
  ...(await importOriginal<typeof NodesApi>()),
  useNode: vi.fn(),
}));
vi.mock('./NodeOverviewTab', () => ({
  NodeOverviewTab: ({ focus }: { focus?: string | null }) => <div data-testid="tab-overview" data-focus={focus ?? ''} />,
}));
vi.mock('./NodeProxyTab', () => ({ NodeProxyTab: () => <div data-testid="tab-proxy" /> }));
vi.mock('./NodeSiteTab', () => ({ NodeSiteTab: () => <div data-testid="tab-site" /> }));
vi.mock('./NodeBlocklist', () => ({ NodeBlocklist: () => <div data-testid="tab-blocklist" /> }));
vi.mock('./NodeMaintenanceTab', () => ({ NodeMaintenanceTab: () => <div data-testid="tab-settings" /> }));

function loaded(engine: 'telemt' | 'tproxy') {
  vi.mocked(useNode).mockReturnValue({
    isLoading: false,
    isError: false,
    data: {
      id: 'n1',
      name: 'Amsterdam',
      hostname: 'ams1.example.com',
      status: 'online',
      online: true,
      engine,
      tls_domain: 'ams1.example.com',
      classic_port: 8443,
      telemt_version: '3.5.9',
      tproxy_version: '',
      agent_version: '2.14.0',
      dirty: false,
    },
  } as unknown as ReturnType<typeof useNode>);
}

const tabNames = () =>
  within(screen.getByRole('navigation', { name: 'Разделы сервера' }))
    .getAllByRole('button')
    .map((b) => b.textContent);

describe('NodeDetailPage tabs', () => {
  beforeEach(() => {
    setLang('ru');
    vi.mocked(useAuth).mockReturnValue({ isWriter: true } as unknown as ReturnType<typeof useAuth>);
  });

  it('has five tabs on a telemt server and no Логи or Пользователи', () => {
    loaded('telemt');
    renderAt('/nodes/n1');
    expect(tabNames()).toEqual(['Состояние', 'Настройки', 'Сайт-прикрытие', 'Блокировки', 'Обслуживание']);
    expect(screen.getByTestId('tab-overview')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Применить изменения/ })).toBeInTheDocument();
    expect(screen.getByText(':8443')).toBeInTheDocument();
  });

  it('leaves Настройки out on a tproxy server', () => {
    loaded('tproxy');
    renderAt('/nodes/n1?section=proxy');
    expect(tabNames()).toEqual(['Состояние', 'Сайт-прикрытие', 'Блокировки', 'Обслуживание']);
    expect(screen.getByTestId('tab-overview')).toBeInTheDocument();
  });

  it.each([
    ['stats', ''],
    ['logs', ''],
    ['profiles', ''],
    ['diagnostics', 'checks'],
    ['web', 'web'],
  ])('sends an old ?section=%s link to Состояние', (old, focus) => {
    loaded('telemt');
    renderAt(`/nodes/n1?section=${old}`);
    expect(screen.getByRole('button', { name: 'Состояние' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByTestId('tab-overview')).toHaveAttribute('data-focus', focus);
  });

  it('opens Обслуживание from its section link', () => {
    loaded('telemt');
    renderAt('/nodes/n1?section=settings');
    expect(screen.getByTestId('tab-settings')).toBeInTheDocument();
  });
});

function renderAt(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/nodes/:id" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('NodeDetailPage', () => {
  beforeEach(() => {
    setLang('en');
    vi.mocked(useAuth).mockReturnValue({ isWriter: true } as unknown as ReturnType<typeof useAuth>);
  });

  it('says the server is gone and leads back to the list when it does not exist', () => {
    vi.mocked(useNode).mockReturnValue({
      isLoading: false,
      isError: true,
      error: new ApiError(404, 'not_found', 'not found'),
      data: undefined,
      refetch: vi.fn(),
    } as unknown as ReturnType<typeof useNode>);

    renderAt('/nodes/00000000-0000-0000-0000-000000000000');

    expect(screen.getByText('Server not found')).toBeInTheDocument();
    expect(screen.queryByText('not found')).toBeNull();
    expect(screen.getByText('Back to servers').closest('a')).toHaveAttribute('href', '/nodes');
  });

  it('offers a retry when the server could not be loaded for another reason', () => {
    vi.mocked(useNode).mockReturnValue({
      isLoading: false,
      isError: true,
      error: new ApiError(500, 'internal', 'boom'),
      data: undefined,
      refetch: vi.fn(),
    } as unknown as ReturnType<typeof useNode>);

    renderAt('/nodes/00000000-0000-0000-0000-000000000000');

    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeInTheDocument();
  });
});
