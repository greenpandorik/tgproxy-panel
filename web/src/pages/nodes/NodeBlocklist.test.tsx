import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { parseBlockLines } from './blocklistLines';
import { NodeBlocklist } from './NodeBlocklist';

import type { Blocklist } from '@/api/blocklist';
import type { Node } from '@/api/types';

vi.mock('@/auth/AuthProvider', () => ({ useAuth: () => ({ isWriter: true }) }));

const LIST: Blocklist = {
  entries: [{ prefix: '198.51.100.0/24', note: 'scanner', added_at: '2026-09-30T10:00:00Z', packets: 42, bytes: 2048 }],
  revision: 3,
  updated_at: '2026-09-30T10:00:00Z',
  max_entries: 2000,
  supported: true,
  live: true,
  synced: true,
  node_revision: 3,
  node_entries: 1,
  dropped_packets: 42,
  dropped_bytes: 2048,
  node_error: '',
};

function reply(status: number, body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function renderCard(put: (body: { entries: { prefix: string; note?: string }[] }) => Promise<Response>, data: Blocklist = LIST) {
  vi.stubGlobal(
    'fetch',
    vi.fn((_input: RequestInfo | URL, init?: RequestInit) =>
      init?.method === 'PUT' ? put(JSON.parse(String(init.body)) as { entries: { prefix: string }[] }) : reply(200, data),
    ),
  );
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <NodeBlocklist node={{ id: 'n1', online: true } as Node} />
    </QueryClientProvider>,
  );
}

describe('NodeBlocklist', () => {
  beforeEach(() => setLang('ru'));

  it('reads one address per line with an optional note', () => {
    expect(parseBlockLines('203.0.113.7 подбор секретов\n\n# комментарий\n2001:db8::/32')).toEqual([
      { line: 1, prefix: '203.0.113.7', note: 'подбор секретов' },
      { line: 4, prefix: '2001:db8::/32', note: '' },
    ]);
  });

  it('shows what the server dropped and adds new lines after the existing ones', async () => {
    const user = userEvent.setup();
    const sent: { prefix: string; note?: string }[][] = [];
    renderCard((body) => {
      sent.push(body.entries);
      return reply(200, { ...LIST, revision: 4, entries: [...LIST.entries, { prefix: '203.0.113.7', note: 'bot', added_at: '2026-10-01T10:00:00Z', packets: 0, bytes: 0 }] });
    });

    expect(await screen.findByText('Действует на сервере')).toBeInTheDocument();
    expect(screen.getByText('42 пакета')).toBeInTheDocument();
    await user.type(screen.getByLabelText('Добавить адреса'), '203.0.113.7 bot');
    await user.click(screen.getByRole('button', { name: 'Добавить' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toEqual([
      { prefix: '198.51.100.0/24', note: 'scanner' },
      { prefix: '203.0.113.7', note: 'bot' },
    ]);
    expect(await screen.findByText('203.0.113.7')).toBeInTheDocument();
  });

  it('puts the panel’s objection next to the line that caused it', async () => {
    const user = userEvent.setup();
    renderCard(() => reply(422, { error: { code: 'validation', message: 'invalid input', fields: { 'entries.2': 'already covered by 198.51.100.0/24' } } }));

    await user.type(await screen.findByLabelText('Добавить адреса'), '203.0.113.7{enter}198.51.100.9');
    await user.click(screen.getByRole('button', { name: 'Добавить (2)' }));
    expect(await screen.findByText('Строка 2: Уже закрыт подсетью 198.51.100.0/24')).toBeInTheDocument();
  });

  it('says so when someone else changed the list first', async () => {
    const user = userEvent.setup();
    let body: unknown;
    renderCard((b) => {
      body = b;
      return reply(409, { error: { code: 'conflict', message: 'the blocklist changed since it was loaded' } });
    });
    await user.type(await screen.findByLabelText('Добавить адреса'), '203.0.113.7');
    await user.click(screen.getByRole('button', { name: 'Добавить' }));
    expect(await screen.findByText(/изменили в другом окне/)).toBeInTheDocument();
    expect(body).toMatchObject({ revision: 3 });
  });

  it('points out rules left on the server from an earlier install', async () => {
    renderCard(() => reply(200, LIST), { ...LIST, entries: [], revision: 0, synced: false, node_revision: 5, node_entries: 2 });
    expect(await screen.findByText(/осталось 2 правила от прошлой установки/)).toBeInTheDocument();
  });

  it('asks for an agent update instead of offering the form', async () => {
    renderCard(() => reply(200, LIST), { ...LIST, entries: [], revision: 0, supported: false, synced: true, dropped_packets: null, dropped_bytes: null, node_revision: null, node_entries: null });
    expect(await screen.findByText(/tgwp-agent upgrade --agent/)).toBeInTheDocument();
    expect(screen.queryByLabelText('Добавить адреса')).toBeNull();
  });
});
