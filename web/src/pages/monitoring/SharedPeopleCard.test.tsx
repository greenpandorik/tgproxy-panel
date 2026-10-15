import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { SharedPeopleCard } from './SharedPeopleCard';

import type { AccessKey } from '@/api/types';

function shared(id: string, label: string, devices: number, extra: Partial<AccessKey> = {}): AccessKey {
  return {
    id,
    label,
    type: 'SHARED',
    state: 'active',
    live: { online: devices > 0, connections: devices * 3, devices, devices_15m: devices, ips: devices },
    ...extra,
  } as AccessKey;
}

function json(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } }));
}

function renderCard(items: AccessKey[]) {
  const fetchMock = vi.fn(() => json({ items, total: items.length, page: 1, per_page: 200 }));
  vi.stubGlobal('fetch', fetchMock);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <SharedPeopleCard />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return fetchMock;
}

describe('SharedPeopleCard', () => {
  beforeEach(() => setLang('ru'));

  it('lists shared users by people online now, most first', async () => {
    const fetchMock = renderCard([
      shared('k-small', 'Friends', 3),
      shared('k-big', 'ComfyPublic', 12_345),
      shared('k-off', 'Office', 0),
      shared('k-gone', 'Old', 50, { state: 'revoked' }),
    ]);

    expect(await screen.findByText('Общие подписки')).toBeInTheDocument();
    const rows = screen.getAllByRole('listitem');
    expect(rows.map((li) => within(li).getByRole('link').textContent)).toEqual([
      'ComfyPublic≈ 12 345соединений: 37 035',
      'Friends≈ 3соединений: 9',
      'Office0',
    ]);
    expect(within(rows[0]).getByRole('link')).toHaveAttribute('href', '/users?user=k-big');
    expect(String(fetchMock.mock.calls[0])).toContain('type=SHARED');
  });

  it('shows nothing without shared users', async () => {
    const fetchMock = renderCard([]);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(screen.queryByText('Общие подписки')).toBeNull();
  });
});
