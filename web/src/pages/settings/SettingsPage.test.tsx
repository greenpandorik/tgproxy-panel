import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AuthProvider } from '@/auth/AuthProvider';

import { SettingsPage } from './SettingsPage';

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function mockApi(role: 'owner' | 'viewer', integrations: { alert_webhook_configured: boolean; metrics_token_set: boolean }) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/auth/me')) return json({ id: 'a1', username: 'root', role, features: { totp: false } });
      if (url.endsWith('/api/v1/settings')) {
        return json({
          apply_interval: 45,
          offline_after: 90,
          telegram_alerts: { enabled: false, bot_token_set: false, chat_id: '', language: 'ru' },
          backup_schedule: { enabled: false, hour: 3, keep: 7 },
          ...integrations,
        });
      }
      return json({ items: [], total: 0 });
    }),
  );
}

function renderPage(entry: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[entry]}>
        <AuthProvider>
          <SettingsPage />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const tabNames = () =>
  within(screen.getByRole('navigation', { name: 'Настройки' }))
    .getAllByRole('button')
    .map((b) => b.textContent);

describe('SettingsPage tabs', () => {
  beforeEach(() => {
    setLang('ru');
    window.localStorage.clear();
  });

  it('puts Integrations right after Notifications and keeps owner tabs for the owner', async () => {
    mockApi('owner', { alert_webhook_configured: false, metrics_token_set: true });
    renderPage('/settings');

    expect(await screen.findByRole('heading', { name: 'Уведомления и опрос серверов' })).toBeInTheDocument();
    await screen.findByRole('button', { name: 'Учётные записи' });
    expect(tabNames()).toEqual([
      'Уведомления',
      'Интеграции',
      'Оформление',
      'Учётные записи',
      'Резервные копии',
      'Мой интерфейс',
      'Пароль и 2FA',
    ]);
  });

  it('shows Integrations to a viewer without the owner tabs', async () => {
    mockApi('viewer', { alert_webhook_configured: true, metrics_token_set: true });
    renderPage('/settings?section=integrations');

    expect(await screen.findByRole('heading', { name: 'Интеграции' })).toBeInTheDocument();
    expect(tabNames()).not.toContain('Учётные записи');
    expect(tabNames()).not.toContain('Резервные копии');
  });

  it('says what is set up and how to export metrics and alerts', async () => {
    mockApi('owner', { alert_webhook_configured: true, metrics_token_set: false });
    renderPage('/settings?section=integrations');

    expect(await screen.findByText('Настроен')).toBeInTheDocument();
    expect(screen.getByText('METRICS_TOKEN не задан: адрес /metrics открыт без токена')).toBeInTheDocument();
    expect(screen.getByText(/job_name: tgwp-panel/)).toBeInTheDocument();
    expect(screen.getByText(/ALERT_WEBHOOK_SECRET=/)).toBeInTheDocument();
    expect(screen.getByText('docs/monitoring.ru.md')).toBeInTheDocument();
  });
});
