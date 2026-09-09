import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { draftStorageKey, readDraft } from '@/lib/drafts';

import { CreateKeyDialog } from './CreateKeyDialog';

import type { Node } from '@/api/types';

const DRAFT_KEY = 'key-create';

function node(id: string, name: string, engine: Node['engine']): Node {
  return {
    id,
    name,
    hostname: `${id}.proxy-demo.net`,
    public_ip: '',
    acme_email: '',
    status: 'online',
    online: true,
    engine,
    tls_domain: '',
    classic_port: 0,
    ad_tag: '',
    telemt_version: '',
    tproxy_version: '',
    agent_version: '',
    max_profiles: 0,
    profile_count: 0,
    dirty: false,
    last_seen_at: null,
    last_apply_at: null,
    created_at: '2026-01-01T00:00:00Z',
    last_check: null,
  };
}

const AMS = node('n-telemt', 'Amsterdam', 'telemt');
const HEL = node('n-tproxy', 'Helsinki', 'tproxy');

function stubNodes(items: Node[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) =>
      String(input).includes('/api/v1/nodes') ? json({ items, total: items.length }) : json({ items: [], total: 0 }),
    ),
  );
}

function json(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } }));
}

/** The dialog the way KeysPage owns it: mounted for good, opened and closed by a flag. */
function Harness() {
  const [open, setOpen] = useState(true);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        reopen
      </button>
      <CreateKeyDialog open={open} onOpenChange={setOpen} onCreated={() => {}} onBatchCreated={() => {}} />
    </>
  );
}

function renderDialog() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <Harness />
    </QueryClientProvider>,
  );
}

describe('CreateKeyDialog drafts', () => {
  beforeEach(() => {
    setLang('ru');
    window.localStorage.clear();
    vi.stubGlobal(
      'fetch',
      vi.fn(() => json({ items: [], total: 0 })),
    );
  });

  it('keeps what was typed when the dialog is cancelled, and offers it back on reopen', async () => {
    const user = userEvent.setup();
    renderDialog();

    const label = await screen.findByLabelText('Метка');
    await user.type(label, 'Команда поддержки');
    expect(screen.queryByRole('status')).toBeNull();

    await user.click(screen.getByRole('button', { name: 'Отмена' }));
    expect(readDraft<{ label: string }>(DRAFT_KEY)?.value.label).toBe('Команда поддержки');

    await user.click(screen.getByRole('button', { name: 'reopen' }));

    // The form itself comes back clean; the draft is an offer, not a surprise.
    expect(await screen.findByLabelText('Метка')).toHaveValue('');
    const banner = await screen.findByRole('status');
    expect(banner).toHaveTextContent(/Черновик от \d{2}:\d{2}/);

    await user.click(screen.getByRole('button', { name: 'Продолжить' }));
    expect(screen.getByLabelText('Метка')).toHaveValue('Команда поддержки');
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('restores the batch tab and its fields from a draft', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByRole('tab', { name: 'Батч' }));
    await user.type(screen.getByLabelText('Префикс'), 'vip');
    await user.click(screen.getByRole('button', { name: 'Отмена' }));
    await user.click(screen.getByRole('button', { name: 'reopen' }));

    // Reopens on the default tab; continuing switches back to the batch.
    expect(await screen.findByLabelText('Метка')).toBeInTheDocument();
    await user.click(await screen.findByRole('button', { name: 'Продолжить' }));
    expect(screen.getByRole('tab', { name: 'Батч' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByLabelText('Префикс')).toHaveValue('vip');
  });

  it('"start over" drops the draft and leaves the empty form', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.type(await screen.findByLabelText('Метка'), 'черновик');
    await user.click(screen.getByRole('button', { name: 'Отмена' }));
    await user.click(screen.getByRole('button', { name: 'reopen' }));

    await user.click(await screen.findByRole('button', { name: 'Начать заново' }));
    expect(screen.queryByRole('status')).toBeNull();
    expect(screen.getByLabelText('Метка')).toHaveValue('');
    expect(window.localStorage.getItem(draftStorageKey(DRAFT_KEY))).toBeNull();
  });

  it('shows no banner when nothing was typed before closing', async () => {
    const user = userEvent.setup();
    renderDialog();

    await screen.findByLabelText('Метка');
    await user.click(screen.getByRole('button', { name: 'Отмена' }));
    await user.click(screen.getByRole('button', { name: 'reopen' }));

    await screen.findByLabelText('Метка');
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull());
    expect(window.localStorage.getItem(draftStorageKey(DRAFT_KEY))).toBeNull();
  });
});

describe('CreateKeyDialog transport', () => {
  beforeEach(() => {
    setLang('ru');
    window.localStorage.clear();
  });

  it('hides the carrier mode when every chosen node runs telemt', async () => {
    const user = userEvent.setup();
    stubNodes([AMS, HEL]);
    renderDialog();

    await user.click(await screen.findByText('Amsterdam'));

    expect(document.querySelector('[data-transport]')).toHaveAttribute('data-transport', 'auto');
    expect(screen.queryByLabelText('Режим передачи')).toBeNull();
    expect(screen.getByText('Подбирается автоматически')).toBeInTheDocument();
  });

  it('keeps the carrier mode when every chosen node runs tproxy', async () => {
    const user = userEvent.setup();
    stubNodes([AMS, HEL]);
    renderDialog();

    await user.click(await screen.findByText('Helsinki'));

    expect(document.querySelector('[data-transport]')).toHaveAttribute('data-transport', 'carrier');
    expect(screen.getByLabelText('Режим передачи')).toBeInTheDocument();
    expect(screen.queryByText('Подбирается автоматически')).toBeNull();
  });

  it('frames the carrier mode as legacy when both engines are chosen', async () => {
    const user = userEvent.setup();
    stubNodes([AMS, HEL]);
    renderDialog();

    await user.click(await screen.findByText('Amsterdam'));
    await user.click(screen.getByText('Helsinki'));

    expect(document.querySelector('[data-transport]')).toHaveAttribute('data-transport', 'legacy');
    expect(screen.getByLabelText('Режим передачи (только tproxy)')).toBeInTheDocument();
    expect(screen.getByText(/уйдёт только на tproxy-ноды/)).toBeInTheDocument();
  });

  it('says nothing about transport until nodes are chosen', async () => {
    stubNodes([AMS, HEL]);
    renderDialog();

    await screen.findByText('Amsterdam');
    expect(document.querySelector('[data-transport]')).toBeNull();
    expect(screen.queryByLabelText('Режим передачи')).toBeNull();
  });
});

describe('CreateKeyDialog limits and summary', () => {
  beforeEach(() => {
    setLang('ru');
    window.localStorage.clear();
    stubNodes([AMS, HEL]);
  });

  it('keeps the limits behind one entry point and opens them in a sheet', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByText('Amsterdam'));
    expect(screen.getByText('Обычный доступ · без ограничений')).toBeInTheDocument();
    expect(screen.queryByLabelText('Квота трафика, ГБ')).toBeNull();

    await user.click(screen.getByText('Лимиты'));

    const quota = await screen.findByLabelText('Квота трафика, ГБ');
    await user.type(quota, '50');
    await user.click(screen.getByRole('button', { name: 'Готово' }));

    expect(await screen.findByText('Задано ограничений: 1')).toBeInTheDocument();
  });

  it('summarises what is about to be created', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.type(await screen.findByLabelText('Метка'), 'Поддержка');
    await user.click(screen.getByText('Amsterdam'));

    expect(screen.getByText(/Общий ключ «Поддержка»/)).toHaveTextContent(
      'нод: 1 · без срока · транспорт автоматически · без ограничений',
    );
  });
});
