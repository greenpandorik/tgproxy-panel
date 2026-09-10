import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { usePublicStatus } from '@/api/status';
import { useUpdateStatus } from '@/api/updates';
import { AuthProvider } from '@/auth/AuthProvider';
import { ThemeProvider } from '@/theme/ThemeProvider';

import { Topbar } from './Topbar';

vi.mock('@/api/updates', () => ({ useUpdateStatus: vi.fn() }));
vi.mock('@/api/status', () => ({ usePublicStatus: vi.fn() }));

function json(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } }));
}

function renderTopbar({ onOpenCommand = () => {} }: { onOpenCommand?: () => void } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <ThemeProvider>
          <AuthProvider>
            <Topbar onOpenMenu={() => {}} onOpenCommand={onOpenCommand} />
          </AuthProvider>
        </ThemeProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Topbar status chips', () => {
  beforeEach(() => {
    setLang('ru');
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes('/auth/me')) return json({ id: 'u1', username: 'root', role: 'owner', features: { totp: false } });
        return json({});
      }),
    );
    vi.mocked(useUpdateStatus).mockReturnValue({
      data: {
        enabled: true,
        current: '1.2.0',
        latest: '1.3.0',
        latest_url: 'https://github.com/greenpandorik/tgproxy-panel/releases/tag/v1.3.0',
        published_at: '',
        stars: 12000,
        repo_url: 'https://github.com/greenpandorik/tgproxy-panel',
        update_available: true,
        checked_at: '',
        stale: false,
      },
    } as unknown as ReturnType<typeof useUpdateStatus>);
    vi.mocked(usePublicStatus).mockReturnValue({
      data: { version: '1.2.0', nodes_total: 2, nodes_online: 2, relay_commit: '' },
    } as unknown as ReturnType<typeof usePublicStatus>);
  });

  it('shows version, stars and the fleet in the header', () => {
    renderTopbar();
    expect(screen.getByTestId('version-chip')).toHaveTextContent('v1.2.0');
    expect(screen.getByTestId('github-chip')).toHaveTextContent('12k');
    expect(screen.getByTestId('nodes-chip')).toHaveTextContent('2/2');
  });

  it('opens the palette from a single search control', () => {
    const onOpenCommand = vi.fn();
    renderTopbar({ onOpenCommand });
    const search = screen.getAllByRole('button', { name: /Поиск|Search/ });
    expect(search).toHaveLength(1);
    search[0].click();
    expect(onOpenCommand).toHaveBeenCalledOnce();
  });

  it('keeps the same details in the user menu, where narrow screens read them', async () => {
    renderTopbar();
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: /root/ }));

    const menu = await screen.findByRole('menu');
    expect(menu).toHaveTextContent('Доступна версия v1.3.0');
    expect(menu).toHaveTextContent('12k');
    expect(menu).toHaveTextContent('2/2');
  });
});
