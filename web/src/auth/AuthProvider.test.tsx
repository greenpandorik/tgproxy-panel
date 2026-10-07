import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { authKeys } from '@/api/auth';
import { UNAUTHORIZED_EVENT } from '@/lib/api';

import { AuthProvider, useAuth } from './AuthProvider';

const OWNER = { id: 'u1', username: 'root', role: 'owner', features: { totp: false } };
const CACHED_KEYS = ['keys', { page: 1 }];

function json(body: unknown, status = 200) {
  return Promise.resolve(
    new Response(body === null ? null : JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }),
  );
}

function LogoutButton() {
  const { logout } = useAuth();
  return (
    <button type="button" onClick={() => void logout()}>
      logout
    </button>
  );
}

function renderWithCache() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(CACHED_KEYS, { items: [{ id: 'k1', subscription_url: 'https://panel.test/s/secret' }] });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/users']}>
        <AuthProvider>
          <LogoutButton />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return qc;
}

describe('AuthProvider', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = String(input);
        if (url.endsWith('/auth/me')) return json(OWNER);
        if (url.endsWith('/auth/logout')) return Promise.resolve(new Response(null, { status: 204 }));
        return json({});
      }),
    );
  });

  it('drops the cached data of the session on logout', async () => {
    const qc = renderWithCache();
    await waitFor(() => expect(qc.getQueryData(authKeys.me)).toEqual(OWNER));

    await userEvent.click(screen.getByRole('button', { name: 'logout' }));
    await waitFor(() => expect(qc.getQueryData(authKeys.me)).toBeNull());
    expect(qc.getQueryData(CACHED_KEYS)).toBeUndefined();
  });

  it('drops the cached data of the session when the session expires', async () => {
    const qc = renderWithCache();
    await waitFor(() => expect(qc.getQueryData(authKeys.me)).toEqual(OWNER));

    act(() => {
      window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
    });
    expect(qc.getQueryData(authKeys.me)).toBeNull();
    expect(qc.getQueryData(CACHED_KEYS)).toBeUndefined();
  });
});
