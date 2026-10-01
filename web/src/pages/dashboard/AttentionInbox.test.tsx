import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { useAuth } from '@/auth/AuthProvider';

import { AttentionInbox } from './AttentionInbox';

import type { Alert } from '@/api/types';

vi.mock('@/auth/AuthProvider', () => ({ useAuth: vi.fn() }));

const NOW = new Date().toISOString();

function alert(id: number, kind: string, node: string): Alert {
  return { id, kind, node_id: `node-${node}`, node_name: node, message: kind, created_at: NOW };
}

let alerts: Alert[] = [];
let resolved: number[][] = [];
let failRead = false;

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function renderInbox() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AttentionInbox />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const items = () => screen.queryAllByTestId('inbox-item');

describe('AttentionInbox', () => {
  beforeEach(() => {
    setLang('en');
    vi.mocked(useAuth).mockReturnValue({ isWriter: true } as unknown as ReturnType<typeof useAuth>);
    alerts = [alert(1, 'node_offline', 'Helsinki'), alert(2, 'apply_failed', 'Frankfurt'), alert(3, 'config_deferred', 'Tokyo')];
    resolved = [];
    failRead = false;
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input), 'http://panel.test');
      if (url.pathname === '/api/v1/alerts/resolve') {
        const ids = (JSON.parse(String(init?.body)) as { ids: number[] }).ids;
        if (failRead) return json({ error: { code: 'internal', message: 'boom' } }, 500);
        resolved.push(ids);
        alerts = alerts.filter((a) => !ids.includes(a.id));
        return json({ resolved: ids.length });
      }
      if (url.pathname === '/api/v1/alerts') return json({ items: alerts });
      return json({ error: { code: 'not_found', message: 'nope' } }, 404);
    }) as typeof fetch;
  });

  it('says each problem in plain words with the server it is about', async () => {
    renderInbox();

    expect(await screen.findByText('Helsinki is not responding')).toBeInTheDocument();
    expect(screen.getByText('Frankfurt')).toBeInTheDocument();
    expect(screen.getByText('Could not apply settings')).toBeInTheDocument();
    expect(within(items()[1]).getByRole('button', { name: 'Apply' })).toBeInTheDocument();
    expect(within(items()[2]).getByRole('button', { name: 'Restart' })).toBeInTheDocument();
    expect(within(items()[0]).getByRole('link', { name: 'Open' })).toHaveAttribute('href', '/nodes/node-Helsinki');
  });

  it('marks one problem read and takes it off the list', async () => {
    renderInbox();
    await screen.findByText('Helsinki is not responding');

    await userEvent.click(within(items()[0]).getByRole('button', { name: 'Mark read' }));

    await waitFor(() => expect(items()).toHaveLength(2));
    expect(resolved).toEqual([[1]]);
  });

  it('marks only the selected problems read', async () => {
    renderInbox();
    await screen.findByText('Helsinki is not responding');

    await userEvent.click(screen.getByRole('checkbox', { name: 'Helsinki is not responding' }));
    await userEvent.click(screen.getByRole('checkbox', { name: /Tokyo/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Mark selected read (2)' }));

    await waitFor(() => expect(items()).toHaveLength(1));
    expect(resolved).toEqual([[1, 3]]);
    expect(screen.getByText('Frankfurt')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Mark all read' })).toBeInTheDocument();
  });

  it('marks everything read and says all is well in the same box', async () => {
    renderInbox();
    await screen.findByText('Helsinki is not responding');

    await userEvent.click(screen.getByRole('button', { name: 'Mark all read' }));

    expect(await screen.findByText('All clear')).toBeInTheDocument();
    expect(resolved).toEqual([[1, 2, 3]]);
    expect(screen.getByRole('heading', { name: 'Needs attention' })).toBeInTheDocument();
  });

  it('selects every problem from the header', async () => {
    renderInbox();
    await screen.findByText('Helsinki is not responding');

    await userEvent.click(screen.getByRole('checkbox', { name: 'Select all' }));

    expect(screen.getByRole('button', { name: 'Mark selected read (3)' })).toBeInTheDocument();
  });

  it('puts the problems back when the panel refuses', async () => {
    failRead = true;
    renderInbox();
    await screen.findByText('Helsinki is not responding');

    await userEvent.click(screen.getByRole('button', { name: 'Mark all read' }));

    await waitFor(() => expect(items()).toHaveLength(3));
    expect(screen.queryByText('All clear')).toBeNull();
  });

  it('shows five problems and offers the rest', async () => {
    alerts = Array.from({ length: 7 }, (_, i) => alert(i + 1, 'node_offline', `S${i + 1}`));
    renderInbox();
    await screen.findByText('S1 is not responding');

    expect(items()).toHaveLength(5);
    await userEvent.click(screen.getByRole('button', { name: 'Show 2 more' }));
    expect(items()).toHaveLength(7);
  });

  it('lets a viewer read the list but not mark it', async () => {
    vi.mocked(useAuth).mockReturnValue({ isWriter: false } as unknown as ReturnType<typeof useAuth>);
    renderInbox();
    await screen.findByText('Helsinki is not responding');

    expect(screen.queryByRole('checkbox')).toBeNull();
    expect(screen.queryByRole('button', { name: /Mark/ })).toBeNull();
  });
});
