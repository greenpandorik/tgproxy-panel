import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { authKeys, forgetSession } from '@/api/auth';
import { blocklistKey, useSaveBlocklist } from '@/api/blocklist';
import { useReorderNodes, nodeKeys } from '@/api/nodes';
import { usePutSubscriptionService } from '@/api/subscriptionService';
import { useBranding } from '@/api/branding';
import { dashboardKeys, useReadAlerts } from '@/api/dashboard';
import { ThemeProvider, useTheme } from '@/theme/ThemeProvider';
import type { Me } from '@/api/types';
import { api, UNAUTHORIZED_EVENT } from '@/lib/api';
import { AuthProvider, useAuth } from './AuthProvider';

const owner: Me = { id: 'owner', username: 'owner', role: 'owner', totp_enabled: false, features: { totp: true } };
const viewer: Me = { ...owner, id: 'viewer', username: 'viewer', role: 'viewer' };
const privateKey = ['keys', { page: 1 }];
const secret = { subscription_url: 'https://panel.test/s/private' };
const clients: QueryClient[] = [];
function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });
}
function deferred() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
function mount<T>(hook: () => T, initial: Me | null = owner) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } } });
  clients.push(qc);
  if (initial) qc.setQueryData(authKeys.me, initial);
  qc.setQueryData(privateKey, secret);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AuthProvider>{children}</AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>
  );
  return { qc, ...renderHook(hook, { wrapper }) };
}

describe('session cache isolation', () => {
  beforeEach(() =>
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(json(viewer))),
    ),
  );
  afterEach(() => {
    clients.splice(0).forEach((qc) => qc.clear());
    vi.unstubAllGlobals();
  });

  it.each(['login', 'totp'] as const)(
    'clears old private data when %s authenticates another account and keeps the live me observer',
    async (step) => {
      const { qc, result } = mount(useAuth);
      const meQuery = qc.getQueryCache().find({ queryKey: authKeys.me });
      await act(async () => {
        if (step === 'login') await result.current.login('viewer', 'password');
        else await result.current.verifyTotp('challenge', { code: '123456' });
      });
      await waitFor(() => expect(result.current.user).toEqual(viewer));
      expect(qc.getQueryData(privateKey)).toBeUndefined();
      expect(qc.getQueryCache().find({ queryKey: authKeys.me })).toBe(meQuery);
    },
  );

  it('forgets the old session when password login returns a TOTP challenge', async () => {
    vi.mocked(fetch).mockResolvedValue(json({ challenge: 'second-step', totp_required: true }));
    const { qc, result } = mount(useAuth);
    await act(async () => {
      await result.current.login('viewer', 'password');
    });
    await waitFor(() => expect(result.current.user).toBeNull());
    expect(qc.getQueryData(privateKey)).toBeUndefined();
  });

  it('ignores a late me response after a global unauthorized event', async () => {
    const pending = deferred();
    vi.mocked(fetch).mockReturnValue(pending.promise);
    const { qc, result } = mount(useAuth, null);
    await waitFor(() => expect(qc.isFetching()).toBe(1));
    act(() => window.dispatchEvent(new Event(UNAUTHORIZED_EVENT)));
    await act(async () => {
      pending.resolve(json(owner));
      await pending.promise;
    });
    expect(qc.getQueryData(authKeys.me)).toBeNull();
    await waitFor(() => expect(result.current.user).toBeNull());
  });

  it('does not restore a session from a login that finishes after expiry', async () => {
    const pending = deferred();
    vi.mocked(fetch).mockReturnValue(pending.promise);
    const { qc, result } = mount(useAuth);
    let login!: Promise<unknown>;
    act(() => {
      login = result.current.login('owner', 'password');
    });
    await waitFor(() => expect(qc.isMutating()).toBe(1));
    act(() => window.dispatchEvent(new Event(UNAUTHORIZED_EVENT)));
    await act(async () => {
      pending.resolve(json(owner));
      await login;
    });
    expect(qc.getQueryData(authKeys.me)).toBeNull();
    await waitFor(() => expect(result.current.user).toBeNull());
  });

  it.each([
    { kind: 'blocklist', nextUser: viewer, account: 'another account' },
    { kind: 'blocklist', nextUser: owner, account: 'the same account' },
    { kind: 'subscription', nextUser: viewer, account: 'another account' },
    { kind: 'subscription', nextUser: owner, account: 'the same account' },
  ])('does not restore private $kind data after a new login to $account', async ({ kind, nextUser }) => {
    const pending = deferred();
    vi.mocked(fetch).mockReturnValue(pending.promise);
    const { qc, result } = mount(() => ({ blocklist: useSaveBlocklist('n1'), subscription: usePutSubscriptionService() }));
    let mutation!: Promise<unknown>;
    act(() => {
      mutation =
        kind === 'blocklist'
          ? result.current.blocklist.mutateAsync({ revision: 1, entries: [] })
          : result.current.subscription.mutateAsync({ public_url: 'https://private.test', hide_on_panel: true });
    });
    await waitFor(() => expect(qc.isMutating()).toBe(1));
    act(() => {
      forgetSession(qc);
      qc.setQueryData(authKeys.me, nextUser);
    });
    await act(async () => {
      pending.resolve(json(secret));
      await mutation;
    });
    expect(qc.getQueryData(kind === 'blocklist' ? blocklistKey('n1') : ['subscription-service'])).toBeUndefined();
    expect(qc.getQueryData(authKeys.me)).toEqual(nextUser);
  });

  it('does not roll back a previous account node list after a pending reorder fails', async () => {
    const pending = deferred();
    vi.mocked(fetch).mockReturnValue(pending.promise);
    const { qc, result } = mount(useReorderNodes);
    qc.setQueryData(nodeKeys.all, { items: [{ id: 'n1', secret: 'private' }, { id: 'n2' }], total: 2 });
    let mutation!: Promise<unknown>;
    act(() => {
      mutation = result.current.mutateAsync(['n2', 'n1']).catch(() => undefined);
    });
    await waitFor(() => expect(qc.getQueryData<{ items: { id: string }[] }>(nodeKeys.all)?.items[0].id).toBe('n2'));
    act(() => {
      forgetSession(qc);
      qc.setQueryData(authKeys.me, viewer);
    });
    await act(async () => {
      pending.resolve(json({ error: { code: 'failure', message: 'failed' } }, 500));
      await mutation;
    });
    expect(qc.getQueryData(nodeKeys.all)).toBeUndefined();
  });

  it('does not expire a newly authenticated session when an old request returns 401', async () => {
    const pending = deferred();
    vi.mocked(fetch).mockImplementation((input) =>
      String(input).endsWith('/private') ? pending.promise : Promise.resolve(json(viewer)),
    );
    const { qc, result } = mount(useAuth);
    const oldRequest = api.get('/api/v1/private').catch(() => undefined);
    await act(async () => {
      await result.current.login('viewer', 'password');
    });
    await waitFor(() => expect(result.current.user).toEqual(viewer));
    await act(async () => {
      pending.resolve(json({ error: { code: 'unauthorized', message: 'expired' } }, 401));
      await oldRequest;
    });
    expect(qc.getQueryData(authKeys.me)).toEqual(viewer);
  });

  it('does not restore a private query when its response arrives after a new login', async () => {
    const pending = deferred();
    vi.mocked(fetch).mockImplementation((input) =>
      String(input).endsWith('/private') ? pending.promise : Promise.resolve(json(viewer)),
    );
    const { qc, result } = mount(useAuth);
    const oldQuery = qc.fetchQuery({ queryKey: ['private'], queryFn: () => api.get('/api/v1/private') }).catch(() => undefined);
    await act(async () => {
      await result.current.login('viewer', 'password');
    });
    await act(async () => {
      pending.resolve(json(secret));
      await oldQuery;
    });
    expect(qc.getQueryData(['private'])).toBeUndefined();
    expect(qc.getQueryData(authKeys.me)).toEqual(viewer);
  });

  it('clears only the QueryClient supplied by the provider', async () => {
    const other = new QueryClient();
    clients.push(other);
    other.setQueryData(privateKey, secret);
    const { qc, result } = mount(useAuth);
    await act(async () => {
      await result.current.logout();
    });
    expect(qc.getQueryData(privateKey)).toBeUndefined();
    expect(other.getQueryData(privateKey)).toEqual(secret);
  });

  it('does not log out or redirect the next account when an old logout finishes', async () => {
    const pending = deferred();
    vi.mocked(fetch).mockImplementation((input) =>
      String(input).endsWith('/auth/logout') ? pending.promise : Promise.resolve(json(viewer)),
    );
    const { qc, result } = mount(() => ({ auth: useAuth(), location: useLocation() }));
    let logout!: Promise<void>;
    act(() => {
      logout = result.current.auth.logout();
    });
    await waitFor(() => expect(qc.isMutating()).toBe(1));
    await act(async () => {
      await result.current.auth.login('viewer', 'password');
    });
    await waitFor(() => expect(result.current.auth.user).toEqual(viewer));
    await act(async () => {
      pending.resolve(new Response(null, { status: 204 }));
      await logout;
    });
    expect(qc.getQueryData(authKeys.me)).toEqual(viewer);
    expect(result.current.location.pathname).toBe('/');
  });

  it('does not roll back a previous account alert list after a pending dismissal fails', async () => {
    const pending = deferred();
    vi.mocked(fetch).mockReturnValue(pending.promise);
    const { qc, result } = mount(useReadAlerts);
    qc.setQueryData(dashboardKeys.alerts, { items: [{ id: 1, title: 'Private node failure' }] });
    let mutation!: Promise<unknown>;
    act(() => {
      mutation = result.current.mutateAsync([1]).catch(() => undefined);
    });
    await waitFor(() => expect(qc.getQueryData<{ items: unknown[] }>(dashboardKeys.alerts)?.items).toEqual([]));
    act(() => {
      forgetSession(qc);
      qc.setQueryData(authKeys.me, viewer);
    });
    await act(async () => {
      pending.resolve(json({ error: { code: 'failure', message: 'failed' } }, 500));
      await mutation;
    });
    expect(qc.getQueryData(dashboardKeys.alerts)).toBeUndefined();
  });

  it('updates the mounted theme and document identity from reloaded public branding on logout', async () => {
    localStorage.clear();
    let name = 'Before logout';
    let theme = 'light';
    vi.mocked(fetch).mockImplementation((input) =>
      Promise.resolve(
        json(String(input).endsWith('/branding') ? { panel_name: name, theme_default: theme, favicon_url: '' } : owner),
      ),
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    clients.push(qc);
    qc.setQueryData(authKeys.me, owner);
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <ThemeProvider>
            <AuthProvider>{children}</AuthProvider>
          </ThemeProvider>
        </MemoryRouter>
      </QueryClientProvider>
    );
    const { result } = renderHook(() => ({ auth: useAuth(), theme: useTheme() }), { wrapper });
    await waitFor(() => expect(document.title).toBe('Before logout'));
    name = 'After logout';
    theme = 'dark';
    await act(async () => {
      await result.current.auth.logout();
    });
    await waitFor(() => expect(document.title).toBe('After logout'));
    expect(result.current.theme.theme).toBe('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');
  });

  it('does not repeatedly refetch public branding after an anonymous 401', async () => {
    let brandingRequests = 0;
    vi.mocked(fetch).mockImplementation((input) => {
      if (String(input).endsWith('/branding')) brandingRequests += 1;
      if (brandingRequests > 3) return new Promise<Response>(() => {});
      return Promise.resolve(json({ error: { code: 'unauthorized', message: 'anonymous' } }, 401));
    });
    const { qc, result } = mount(() => ({ auth: useAuth(), branding: useBranding() }), null);
    await waitFor(() => expect(result.current.branding.isError).toBe(true));
    expect(brandingRequests).toBe(1);
    expect(qc.getQueryData(authKeys.me)).toBeNull();
  });

  it('refreshes public branding on account transitions without disconnecting its observer', async () => {
    let panelName = 'First panel';
    vi.mocked(fetch).mockImplementation((input) =>
      Promise.resolve(json(String(input).endsWith('/branding') ? { panel_name: panelName } : viewer)),
    );
    const { result } = mount(() => ({ auth: useAuth(), branding: useBranding() }));
    await waitFor(() => expect(result.current.branding.data?.panel_name).toBe('First panel'));
    panelName = 'Updated panel';
    await act(async () => {
      await result.current.auth.login('viewer', 'password');
    });
    await waitFor(() => expect(result.current.branding.data?.panel_name).toBe('Updated panel'));
  });
});
