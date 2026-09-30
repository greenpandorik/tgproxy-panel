import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AuditPage } from './AuditPage';

import type { AuditEntry } from '@/api/types';

const ENTRY: AuditEntry = {
  id: 1,
  username: 'root',
  action: 'auth.login',
  target_type: 'admin',
  target_id: '1c403781-0000-0000-0000-000000000000',
  meta: null,
  ip: '127.0.0.1',
  created_at: '2026-09-30T20:33:00Z',
};

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AuditPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AuditPage', () => {
  beforeEach(() => {
    setLang('en');
    globalThis.fetch = vi.fn((input: RequestInfo | URL) => {
      const url = new URL(String(input), 'http://panel.test');
      if (url.pathname === '/api/v1/nodes') return json({ items: [] });
      if (url.pathname === '/api/v1/audit') {
        return url.searchParams.has('user') ? json({ items: [], total: 0 }) : json({ items: [ENTRY], total: 1 });
      }
      return json({ error: { code: 'not_found', message: 'nope' } }, 404);
    }) as typeof fetch;
  });

  it('offers to reset the filters when nothing matches them', async () => {
    const user = userEvent.setup();
    renderPage();

    expect((await screen.findAllByText('Signed in')).length).toBeGreaterThan(0);
    expect(screen.queryByRole('button', { name: 'Reset filters' })).toBeNull();

    await user.type(screen.getByRole('textbox', { name: 'Account' }), 'nobody');

    expect(await screen.findByText('Nothing matches these filters')).toBeInTheDocument();
    const resets = screen.getAllByRole('button', { name: 'Reset filters' });
    await user.click(resets[resets.length - 1]);

    expect((await screen.findAllByText('Signed in')).length).toBeGreaterThan(0);
    expect(screen.getByRole('textbox', { name: 'Account' })).toHaveValue('');
    expect(screen.queryByRole('button', { name: 'Reset filters' })).toBeNull();
  });
});
