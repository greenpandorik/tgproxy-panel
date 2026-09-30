import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AuthProvider } from '@/auth/AuthProvider';

import { Toaster } from '@/components/ui/toast';

import { PanelForm } from './PanelForm';

import type { Settings } from '@/api/types';

const ME = { id: 'u1', username: 'root', role: 'owner', totp_enabled: false, features: { totp: true } };

const SETTINGS: Settings = {
  apply_interval: 45,
  offline_after: 90,
  telegram_alerts: { enabled: true, bot_token_set: true, chat_id: '42', language: 'ru' },
  backup_schedule: { enabled: false, hour: 3, keep: 7 },
};

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

beforeEach(() => {
  setLang('en');
  localStorage.clear();
  document.cookie = 'tgwp_csrf=test-csrf';
});

it('sends the chosen notification language with the test message and the saved settings', async () => {
  const bodies: Record<string, unknown> = {};
  globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    if (init?.body) bodies[`${init.method} ${path}`] = JSON.parse(String(init.body));
    if (path.endsWith('/auth/me')) return json(ME);
    if (path.endsWith('/settings/telegram/test')) return json({ ok: true });
    if (path.endsWith('/settings')) return json(SETTINGS);
    return json({ error: { code: 'not_found', message: 'nope' } }, 404);
  }) as typeof fetch;

  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AuthProvider>
          <PanelForm />
          <Toaster />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );

  const select = await screen.findByLabelText('Notification language');
  expect(select).toHaveValue('ru');
  await userEvent.selectOptions(select, 'en');

  await userEvent.click(screen.getByRole('button', { name: 'Send test message' }));
  await waitFor(() => expect(bodies['POST /api/v1/settings/telegram/test']).toMatchObject({ language: 'en' }));

  await userEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() =>
    expect(bodies['PUT /api/v1/settings']).toMatchObject({ telegram_alerts: { chat_id: '42', language: 'en' } }),
  );
});
