import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AuthProvider } from '@/auth/AuthProvider';

import { KeyLinkDialog } from './KeyLinkDialog';

import type { NodeLinkGroup } from '@/api/types';

const KEY_ID = 'k-1';

const TELEMT_GROUP: NodeLinkGroup = {
  node_id: 'n-telemt',
  node_name: 'Amsterdam',
  hostname: 'ams1.proxy-demo.net',
  engine: 'telemt',
  links: [
    { kind: 'web', tme: 'https://t.me/webproxy?server=ams1.proxy-demo.net&secret=aa', tg: 'tg://webproxy?server=ams1&secret=aa' },
    {
      kind: 'tls',
      tme: 'https://t.me/proxy?server=ams1.proxy-demo.net&port=8443&secret=eeaa',
      tg: 'tg://proxy?server=ams1&port=8443&secret=eeaa',
    },
  ],
};

const TPROXY_GROUP: NodeLinkGroup = {
  node_id: 'n-tproxy',
  node_name: 'Helsinki',
  hostname: 'hel1.proxy-demo.net',
  engine: 'tproxy',
  links: [{ kind: 'web', tme: 'https://t.me/webproxy?server=hel1.proxy-demo.net&secret=bb', tg: 'tg://webproxy?server=hel1&secret=bb' }],
};

const KEY = {
  id: KEY_ID,
  label: 'Команда поддержки',
  type: 'SHARED',
  owner_label: '',
  status: 'active',
  carrier_mode: 'https',
  limits: {},
  telemt_limits: {},
  expires_at: null,
  revoked_at: null,
  note: '',
  created_at: '2026-01-01T00:00:00Z',
  nodes: [],
  client_support: { desktop: 'stable', android: 'experimental', ios: 'planned' },
  subscription_active: false,
  traffic_30d: 0,
};

function json(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } }));
}

function mockApi(groups: NodeLinkGroup[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes(`/keys/${KEY_ID}/links`)) {
        return json({ items: groups, client_support: KEY.client_support });
      }
      if (url.includes(`/keys/${KEY_ID}`)) return json(KEY);
      if (url.includes('/auth/me')) return json({ id: 'u1', username: 'root', role: 'owner', features: { totp: false } });
      return json({});
    }),
  );
}

function renderDialog() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AuthProvider>
          <KeyLinkDialog open onOpenChange={() => {}} keyId={KEY_ID} />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('KeyLinkDialog link kinds', () => {
  beforeEach(() => setLang('ru'));

  it('gives a telemt node one tab per link kind and shows the WEB link first', async () => {
    mockApi([TELEMT_GROUP]);
    renderDialog();

    expect(await screen.findByRole('tab', { name: 'WEB' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Fake-TLS' })).toBeInTheDocument();
    expect(screen.getByDisplayValue(TELEMT_GROUP.links[0].tme)).toBeInTheDocument();
    // The Fake-TLS link lives behind its own tab, not stacked under the WEB one.
    expect(screen.queryByDisplayValue(TELEMT_GROUP.links[1].tme)).toBeNull();
    expect(screen.queryByText('только WEB на этом движке')).toBeNull();
  });

  it('switches to the Fake-TLS link and its QR when that tab is chosen', async () => {
    mockApi([TELEMT_GROUP]);
    renderDialog();

    await userEvent.click(await screen.findByRole('tab', { name: 'Fake-TLS' }));

    expect(await screen.findByDisplayValue(TELEMT_GROUP.links[1].tme)).toBeInTheDocument();
    const qr = screen.getByAltText(/Fake-TLS/);
    expect(qr).toHaveAttribute('src', expect.stringContaining('kind=tls'));
  });

  it('shows a tproxy node its single WEB link with a note instead of tabs', async () => {
    mockApi([TPROXY_GROUP]);
    renderDialog();

    expect(await screen.findByText('только WEB на этом движке')).toBeInTheDocument();
    expect(screen.queryByRole('tab')).toBeNull();
    expect(screen.getByDisplayValue(TPROXY_GROUP.links[0].tme)).toBeInTheDocument();
  });
});
