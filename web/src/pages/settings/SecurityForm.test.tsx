import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AuthProvider } from '@/auth/AuthProvider';

import { SecurityForm } from './SecurityForm';

const ME = { id: 'u1', username: 'root', role: 'owner', totp_enabled: false, features: { totp: false } };

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function renderForm() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AuthProvider>
          <SecurityForm />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('SecurityForm', () => {
  beforeEach(() => {
    setLang('en');
    document.cookie = 'tgwp_csrf=test-csrf';
    globalThis.fetch = vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith('/auth/me')) return json(ME);
      if (path.endsWith('/me/password')) {
        return json({ error: { code: 'validation', message: 'validation failed', fields: { current: 'wrong password' } } }, 422);
      }
      return json({ error: { code: 'not_found', message: 'nope' } }, 404);
    }) as typeof fetch;
  });

  it('asks for the current password when it is left empty', async () => {
    const user = userEvent.setup();
    renderForm();

    await user.click(await screen.findByRole('button', { name: 'Change password' }));

    expect(await screen.findByText('Required field')).toBeInTheDocument();
  });

  it('says the current password is wrong when the server rejects it', async () => {
    const user = userEvent.setup();
    renderForm();

    await user.type(await screen.findByLabelText('Current password'), 'not-my-password');
    await user.type(screen.getByLabelText('New password'), 'a-new-password-1');
    await user.type(screen.getByLabelText('Confirm new password'), 'a-new-password-1');
    await user.click(screen.getByRole('button', { name: 'Change password' }));

    expect(await screen.findByText('Wrong current password')).toBeInTheDocument();
    expect(screen.queryByText('Required field')).toBeNull();
  });
});
