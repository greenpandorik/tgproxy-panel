import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { SubscriptionPageForm } from './SubscriptionPageForm';

vi.mock('@/auth/AuthProvider', () => ({ useAuth: () => ({ isOwner: true, isWriter: true }) }));

const PAGE = {
  language: 'ru',
  title_ru: '',
  title_en: '',
  intro_ru: '',
  intro_en: '',
  tls_button_ru: '',
  tls_button_en: '',
  tls_note_ru: '',
  tls_note_en: '',
  web_button_ru: '',
  web_button_en: '',
  web_note_ru: 'Старая подпись',
  web_note_en: '',
  support_label_ru: '',
  support_label_en: '',
  support_url: '',
  hide_support: false,
  show_fake_tls: true,
  show_web: true,
  show_backup_domains: true,
  show_status: true,
  show_qr: true,
  hidden_nodes: [],
};

function reply(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function renderForm(sent: unknown[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path.endsWith('/api/v1/settings') && init?.method === 'PUT') {
        sent.push(JSON.parse(String(init.body)));
        return reply({ apply_interval: 45, subscription_page: PAGE });
      }
      if (path.endsWith('/api/v1/settings')) return reply({ apply_interval: 45, subscription_page: PAGE });
      if (path.endsWith('/api/v1/branding')) return reply({ support_link: 'https://t.me/branding_help' });
      if (path.includes('/api/v1/nodes')) return reply({ items: [], total: 0 });
      return reply({});
    }),
  );
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <SubscriptionPageForm />
    </QueryClientProvider>,
  );
}

describe('SubscriptionPageForm', () => {
  beforeEach(() => setLang('ru'));

  it('shows the standard texts as placeholders in the chosen language and resets an own text', async () => {
    const user = userEvent.setup();
    const sent: unknown[] = [];
    renderForm(sent);

    const webNote = await screen.findByLabelText('Подпись под кнопкой WEB');
    expect(webNote).toHaveValue('Старая подпись');
    expect(screen.getByLabelText('Кнопка Fake-TLS')).toHaveAttribute('placeholder', 'Подключить через Fake-TLS');

    await user.click(screen.getByRole('button', { name: 'Вернуть стандартный' }));
    expect(screen.getByLabelText('Подпись под кнопкой WEB')).toHaveValue('');
    expect(screen.getByLabelText('Подпись под кнопкой WEB')).toHaveAttribute(
      'placeholder',
      'Для Telegram на Android и компьютере. Похоже на обычный сайт',
    );

    await user.click(screen.getByRole('radio', { name: 'На английском' }));
    expect(screen.getByLabelText('Кнопка Fake-TLS')).toHaveAttribute('placeholder', 'Connect via Fake-TLS');
    await user.type(screen.getByLabelText('Кнопка Fake-TLS'), '  Connect  ');

    await user.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    const body = (sent[0] as { subscription_page: Record<string, unknown> }).subscription_page;
    expect(body.web_note_ru).toBe('');
    expect(body.tls_button_en).toBe('Connect');
    expect(body).not.toHaveProperty('platform');
    expect(body).not.toHaveProperty('show_guide');
  });

  it('falls back to the branding link and refuses an unsafe support link', async () => {
    const user = userEvent.setup();
    renderForm([]);

    const url = await screen.findByLabelText('Ссылка');
    await waitFor(() => expect(screen.getByText(/https:\/\/t\.me\/branding_help/)).toBeInTheDocument());
    await user.type(url, 'javascript:alert(1)');
    expect(await screen.findByText(/Нужна ссылка, которая начинается с https/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeDisabled();

    await user.click(screen.getByRole('switch', { name: 'Показывать кнопку поддержки' }));
    expect(screen.queryByLabelText('Ссылка')).toBeNull();
    await waitFor(() => expect(screen.getByRole('button', { name: 'Сохранить' })).toBeEnabled());
  });
});
