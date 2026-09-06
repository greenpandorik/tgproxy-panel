import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AuthProvider } from '@/auth/AuthProvider';

import { LoginPage } from './LoginPage';

/**
 * Routes fetch calls by path so the test drives the real hooks (useLogin ->
 * useTotpVerify) rather than a mocked AuthProvider: the point of the test is that
 * a `totp_required` answer is not treated as a session.
 */
function stubFetch(handlers: Record<string, () => { status: number; body: unknown }>) {
  return vi.fn((input: RequestInfo | URL) => {
    const path = String(input);
    const key = Object.keys(handlers).find((k) => path.endsWith(k));
    const { status, body } = key ? handlers[key]() : { status: 404, body: { error: { code: 'not_found', message: 'nope' } } };
    return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
  });
}

const ME = { id: 'u1', username: 'root', role: 'owner', totp_enabled: true, features: { totp: true } };

function renderLogin() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/login']}>
        <AuthProvider>
          <LoginPage />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('LoginPage two-factor step', () => {
  beforeEach(() => {
    setLang('en');
    document.cookie = 'tgwp_csrf=test-csrf';
  });

  it('asks for a code instead of signing in when the account has TOTP enrolled', async () => {
    const user = userEvent.setup();
    let verified: unknown = null;
    globalThis.fetch = stubFetch({
      '/auth/me': () => ({ status: 401, body: { error: { code: 'unauthorized', message: 'no' } } }),
      '/auth/login': () => ({ status: 200, body: { totp_required: true, challenge: 'chal-1' } }),
      '/auth/totp/verify': () => {
        verified = true;
        return { status: 200, body: ME };
      },
      '/branding': () => ({ status: 200, body: { panel_name: 'Panel' } }),
    }) as typeof fetch;

    renderLogin();

    await user.type(await screen.findByLabelText('Username'), 'root');
    await user.type(screen.getByLabelText('Password'), 'pass-123456');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    // The challenge swaps the card body; no navigation happened.
    expect(await screen.findByLabelText('Code from the app')).toBeInTheDocument();
    expect(verified).toBeNull();

    await user.type(screen.getByLabelText('Code from the app'), '123456');
    await user.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => expect(verified).toBe(true));
  });

  // An expired challenge is dead: no code will ever be accepted against it. Telling
  // the user their code was wrong sends them off checking their phone's clock,
  // when the only thing that helps is starting the sign-in again.
  it('offers a restart instead of "wrong code" when the challenge has expired', async () => {
    const user = userEvent.setup();
    globalThis.fetch = stubFetch({
      '/auth/me': () => ({ status: 401, body: { error: { code: 'unauthorized', message: 'no' } } }),
      '/auth/login': () => ({ status: 200, body: { totp_required: true, challenge: 'chal-1' } }),
      '/auth/totp/verify': () => ({
        status: 401,
        body: { error: { code: 'challenge_expired', message: 'this sign-in attempt expired, start again' } },
      }),
      '/branding': () => ({ status: 200, body: { panel_name: 'Panel' } }),
    }) as typeof fetch;

    renderLogin();

    await user.type(await screen.findByLabelText('Username'), 'root');
    await user.type(screen.getByLabelText('Password'), 'pass-123456');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    await user.type(await screen.findByLabelText('Code from the app'), '123456');
    await user.click(screen.getByRole('button', { name: 'Confirm' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('This sign-in attempt expired. Start over and sign in again.');
    // The primary action becomes the way out, and Confirm is gone: pressing it
    // again could only fail.
    expect(screen.queryByRole('button', { name: 'Confirm' })).toBeNull();
    await user.click(screen.getAllByRole('button', { name: 'Start over' })[0]);
    expect(await screen.findByLabelText('Username')).toBeInTheDocument();
  });

  it('shows "wrong code" for an ordinary bad code, and keeps the Confirm button', async () => {
    const user = userEvent.setup();
    globalThis.fetch = stubFetch({
      '/auth/me': () => ({ status: 401, body: { error: { code: 'unauthorized', message: 'no' } } }),
      '/auth/login': () => ({ status: 200, body: { totp_required: true, challenge: 'chal-1' } }),
      '/auth/totp/verify': () => ({ status: 401, body: { error: { code: 'invalid_code', message: 'wrong code' } } }),
      '/branding': () => ({ status: 200, body: { panel_name: 'Panel' } }),
    }) as typeof fetch;

    renderLogin();

    await user.type(await screen.findByLabelText('Username'), 'root');
    await user.type(screen.getByLabelText('Password'), 'pass-123456');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    await user.type(await screen.findByLabelText('Code from the app'), '123456');
    await user.click(screen.getByRole('button', { name: 'Confirm' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Wrong code. Try again.');
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeInTheDocument();
  });

  it('switches the second step to a recovery code', async () => {
    const user = userEvent.setup();
    let sent: Record<string, unknown> | null = null;
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path.endsWith('/auth/login')) {
        return Promise.resolve(
          new Response(JSON.stringify({ totp_required: true, challenge: 'chal-1' }), {
            status: 200,
            headers: { 'content-type': 'application/json' },
          }),
        );
      }
      if (path.endsWith('/auth/totp/verify')) {
        sent = JSON.parse(String(init?.body)) as Record<string, unknown>;
        return Promise.resolve(
          new Response(JSON.stringify(ME), { status: 200, headers: { 'content-type': 'application/json' } }),
        );
      }
      return Promise.resolve(
        new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'no' } }), {
          status: 401,
          headers: { 'content-type': 'application/json' },
        }),
      );
    }) as typeof fetch;

    renderLogin();

    await user.type(await screen.findByLabelText('Username'), 'root');
    await user.type(screen.getByLabelText('Password'), 'pass-123456');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    await user.click(await screen.findByRole('button', { name: 'Enter a recovery code' }));
    await user.type(screen.getByLabelText('Recovery code'), 'ABC12-DEF34');
    await user.click(screen.getByRole('button', { name: 'Confirm' }));

    // Recovery codes are stored lower-case, so the field is normalised before it is sent.
    await waitFor(() => expect(sent).toEqual({ challenge: 'chal-1', recovery_code: 'abc12-def34' }));
  });
});

/**
 * Below 900px the split layout collapses to the form alone: the status panel is
 * not rendered at all (rather than hidden with CSS), so a phone never pays for
 * the public-status request or scrolls past a card it cannot use.
 */
describe('LoginPage split layout', () => {
  function mockWidth(matches: boolean) {
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      writable: true,
      value: (query: string) => ({
        matches,
        media: query,
        onchange: null,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }),
    });
  }

  beforeEach(() => {
    setLang('en');
    globalThis.fetch = stubFetch({
      '/auth/me': () => ({ status: 401, body: { error: { code: 'unauthorized', message: 'no' } } }),
      '/status/public': () => ({
        status: 200,
        body: { version: '1.0.0', nodes_total: 3, nodes_online: 2, relay_commit: '52a5feb' },
      }),
      '/branding': () => ({ status: 200, body: { panel_name: 'Panel' } }),
    }) as typeof fetch;
  });

  it('renders the status panel beside the form on a wide screen', async () => {
    mockWidth(true);
    renderLogin();
    expect(await screen.findByLabelText('Username')).toBeInTheDocument();
    expect(screen.getByTestId('login-status-panel')).toBeInTheDocument();
  });

  it('drops the status panel below 900px and keeps the form', async () => {
    mockWidth(false);
    renderLogin();
    expect(await screen.findByLabelText('Username')).toBeInTheDocument();
    expect(screen.queryByTestId('login-status-panel')).toBeNull();
  });
});
