import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { draftStorageKey, readDraft } from '@/lib/drafts';

import { CreateKeyDialog } from './CreateKeyDialog';

const DRAFT_KEY = 'key-create';

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
