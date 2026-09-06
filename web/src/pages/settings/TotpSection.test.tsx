import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AuthProvider } from '@/auth/AuthProvider';

import { TotpSection } from './TotpSection';

const CODES = [
  'aaaaa-11111',
  'bbbbb-22222',
  'ccccc-33333',
  'ddddd-44444',
  'eeeee-55555',
  'fffff-66666',
  'ggggg-77777',
  'hhhhh-88888',
];

const ME = { id: 'u1', username: 'root', role: 'owner', totp_enabled: false, features: { totp: true } };

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AuthProvider>
          <TotpSection />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('TotpSection recovery codes dialog', () => {
  beforeEach(() => {
    setLang('en');
    document.cookie = 'tgwp_csrf=test-csrf';
    globalThis.fetch = vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith('/auth/me')) return json(ME);
      if (path.endsWith('/auth/totp/setup')) {
        return json({
          secret: 'JBSWY3DPEHPK3PXP',
          otpauth_url: 'otpauth://totp/x:root',
          qr_data_uri: 'data:image/png;base64,AAAA',
        });
      }
      if (path.endsWith('/auth/totp/confirm')) return json({ recovery_codes: CODES });
      return json({ error: { code: 'not_found', message: 'nope' } }, 404);
    }) as typeof fetch;
  });

  /** Walks the enable flow up to the point where the codes are on screen. */
  async function openCodes(user: ReturnType<typeof userEvent.setup>) {
    await user.click(await screen.findByRole('button', { name: 'Turn on' }));
    await user.type(await screen.findByLabelText('Code from the app'), '123456');
    await user.type(await screen.findByLabelText('Your password'), 'pass-123456');
    await user.click(screen.getByRole('button', { name: 'Confirm and turn on' }));
    expect(await screen.findByText(CODES[0])).toBeInTheDocument();
  }

  // A live session alone must not be able to enrol a factor: without the password
  // a hijacked tab could attach a stranger's authenticator to the account.
  it('will not confirm without the password', async () => {
    const user = userEvent.setup();
    renderSection();

    await user.click(await screen.findByRole('button', { name: 'Turn on' }));
    await user.type(await screen.findByLabelText('Code from the app'), '123456');
    expect(screen.getByRole('button', { name: 'Confirm and turn on' })).toBeDisabled();

    await user.type(await screen.findByLabelText('Your password'), 'pass-123456');
    expect(screen.getByRole('button', { name: 'Confirm and turn on' })).toBeEnabled();
  });

  it('sends the password with the code and reports a wrong one on the password field', async () => {
    const sent: string[] = [];
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path.endsWith('/auth/me')) return json(ME);
      if (path.endsWith('/auth/totp/setup')) {
        return json({ secret: 'JBSWY3DPEHPK3PXP', otpauth_url: 'otpauth://totp/x:root', qr_data_uri: 'data:image/png;base64,AAAA' });
      }
      if (path.endsWith('/auth/totp/confirm')) {
        sent.push(String(init?.body));
        return json({ error: { code: 'validation_failed', message: 'no', fields: { password: 'wrong password' } } }, 422);
      }
      return json({ error: { code: 'not_found', message: 'nope' } }, 404);
    }) as typeof fetch;

    const user = userEvent.setup();
    renderSection();
    await user.click(await screen.findByRole('button', { name: 'Turn on' }));
    await user.type(await screen.findByLabelText('Code from the app'), '123456');
    await user.type(await screen.findByLabelText('Your password'), 'wrong');
    await user.click(screen.getByRole('button', { name: 'Confirm and turn on' }));

    expect(await screen.findByText('Wrong current password')).toBeInTheDocument();
    expect(JSON.parse(sent[0])).toEqual({ password: 'wrong', code: '123456' });
  });

  it('refuses to close until the codes are acknowledged, and ignores Escape', async () => {
    const user = userEvent.setup();
    renderSection();
    await openCodes(user);

    const done = screen.getByRole('button', { name: 'I saved the codes' });
    expect(done).toBeDisabled();

    // Escape is the reflex that would throw away the only copy of the codes.
    await user.keyboard('{Escape}');
    expect(screen.getByText(CODES[0])).toBeInTheDocument();

    // There is no close (X) button to click either.
    expect(screen.queryByRole('button', { name: 'Close' })).toBeNull();

    await user.click(screen.getByRole('checkbox', { name: 'I have saved these codes somewhere safe' }));
    expect(done).toBeEnabled();

    await user.click(done);
    await waitFor(() => expect(screen.queryByText(CODES[0])).toBeNull());
  });

  it('lists every code and offers a download', async () => {
    const user = userEvent.setup();
    renderSection();
    await openCodes(user);

    for (const code of CODES) {
      expect(screen.getByText(code)).toBeInTheDocument();
    }
    expect(screen.getByRole('button', { name: 'Download .txt' })).toBeInTheDocument();
  });
});
