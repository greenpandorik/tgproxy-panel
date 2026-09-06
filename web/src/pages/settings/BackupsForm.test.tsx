import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AuthProvider } from '@/auth/AuthProvider';

import { Toaster } from '@/components/ui/toast';

import { BackupsForm } from './BackupsForm';

import type { Backup, Settings } from '@/api/types';

const ME = { id: 'u1', username: 'root', role: 'owner', totp_enabled: false, features: { totp: true } };

const SETTINGS: Settings = {
  apply_interval: 45,
  offline_after: 90,
  telegram_alerts: { enabled: false, bot_token_set: false, chat_id: '' },
  backup_schedule: { enabled: false, hour: 3, keep: 7 },
};

const EXISTING: Backup = {
  id: 'b-1',
  name: 'tgwp-20260904T030000Z-scheduled.dump',
  size: 5_242_880,
  kind: 'scheduled',
  created_at: '2026-09-04T03:00:00Z',
};

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function renderForm() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AuthProvider>
          <BackupsForm />
          <Toaster />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BackupsForm', () => {
  beforeEach(() => {
    setLang('en');
    document.cookie = 'tgwp_csrf=test-csrf';
  });

  it('lists backups with a download link and hides the schedule fields until it is on', async () => {
    globalThis.fetch = vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith('/auth/me')) return json(ME);
      if (path.endsWith('/backups')) return json({ items: [EXISTING] });
      if (path.endsWith('/settings')) return json(SETTINGS);
      return json({ error: { code: 'not_found', message: 'nope' } }, 404);
    }) as typeof fetch;

    renderForm();

    const rows = await screen.findAllByText(EXISTING.name);
    expect(rows.length).toBeGreaterThan(0);
    expect(screen.getAllByText('5.0 MB').length).toBeGreaterThan(0);
    const link = screen.getAllByRole('link', { name: 'Download' })[0];
    expect(link).toHaveAttribute('href', '/api/v1/backups/b-1/download');

    // Schedule is off in SETTINGS, so its fields stay out of the way.
    expect(screen.queryByLabelText('Keep dumps')).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('switch'));
    expect(await screen.findByLabelText('Keep dumps')).toBeInTheDocument();
  });

  it('creates a backup and reports a run that is already in progress', async () => {
    let created = false;
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path.endsWith('/auth/me')) return json(ME);
      if (path.endsWith('/settings')) return json(SETTINGS);
      if (path.endsWith('/backups') && init?.method === 'POST') {
        if (created) return json({ error: { code: 'backup_running', message: 'a backup is already running' } }, 409);
        created = true;
        return json({ id: 'b-2', name: 'tgwp-new-manual.dump', size: 1024, kind: 'manual', created_at: '2026-09-05T12:00:00Z' }, 201);
      }
      if (path.endsWith('/backups')) {
        return json({ items: created ? [{ id: 'b-2', name: 'tgwp-new-manual.dump', size: 1024, kind: 'manual', created_at: '2026-09-05T12:00:00Z' }] : [] });
      }
      return json({ error: { code: 'not_found', message: 'nope' } }, 404);
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    renderForm();
    expect(await screen.findByText(/No backups yet/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Create backup' }));
    await waitFor(() => expect(screen.getAllByText('tgwp-new-manual.dump').length).toBeGreaterThan(0));

    await userEvent.click(screen.getByRole('button', { name: 'Create backup' }));
    expect(await screen.findByText('A backup is already running. Wait for it to finish.')).toBeInTheDocument();
  });

  it('deletes a backup after the confirmation', async () => {
    let deleted = false;
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path.endsWith('/auth/me')) return json(ME);
      if (path.endsWith('/settings')) return json(SETTINGS);
      if (path.endsWith('/backups/b-1') && init?.method === 'DELETE') {
        deleted = true;
        return Promise.resolve(new Response(null, { status: 204 }));
      }
      if (path.endsWith('/backups')) return json({ items: deleted ? [] : [EXISTING] });
      return json({ error: { code: 'not_found', message: 'nope' } }, 404);
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    renderForm();
    await screen.findAllByText(EXISTING.name);
    await userEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0]);

    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Delete' }));

    await waitFor(() => expect(deleted).toBe(true));
    expect(await screen.findByText(/No backups yet/)).toBeInTheDocument();
  });

  it('saves the schedule as an object', async () => {
    let saved: unknown = null;
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path.endsWith('/auth/me')) return json(ME);
      if (path.endsWith('/backups')) return json({ items: [] });
      if (path.endsWith('/settings') && init?.method === 'PUT') {
        saved = JSON.parse(String(init.body));
        return json(SETTINGS);
      }
      if (path.endsWith('/settings')) return json(SETTINGS);
      return json({ error: { code: 'not_found', message: 'nope' } }, 404);
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    renderForm();
    await userEvent.click(await screen.findByRole('switch'));
    const keep = await screen.findByLabelText('Keep dumps');
    await userEvent.clear(keep);
    await userEvent.type(keep, '14');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(saved).toEqual({ backup_schedule: { enabled: true, hour: 3, keep: 14 } }));
  });
});
