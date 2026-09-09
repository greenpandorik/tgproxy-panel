import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { LoginStatusPanel } from './LoginStatusPanel';

function renderPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <LoginStatusPanel />
    </QueryClientProvider>,
  );
}

const STATUS = { version: '1.0.0', nodes_total: 3, nodes_online: 2, relay_commit: '52a5feb' };

function stubFetch(status: () => { status: number; body: unknown } | 'hang') {
  return vi.fn((input: RequestInfo | URL) => {
    const path = String(input);
    if (path.endsWith('/status/public')) {
      const answer = status();
      if (answer === 'hang') return new Promise<Response>(() => {});
      return Promise.resolve(
        new Response(JSON.stringify(answer.body), {
          status: answer.status,
          headers: { 'content-type': 'application/json' },
        }),
      );
    }
    return Promise.resolve(
      new Response(JSON.stringify({ panel_name: 'Panel' }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }),
    );
  }) as typeof fetch;
}

describe('LoginStatusPanel', () => {
  beforeEach(() => {
    setLang('en');
  });

  it('shows the four public facts and nothing else about the fleet', async () => {
    globalThis.fetch = stubFetch(() => ({ status: 200, body: STATUS }));

    renderPanel();

    expect(await screen.findByText('2 / 3')).toBeInTheDocument();
    expect(screen.getByText('nodes online')).toBeInTheDocument();
    expect(screen.getByText('52a5feb')).toBeInTheDocument();
    // The version appears in the card and again in the panel footer.
    expect(screen.getAllByText('v1.0.0').length).toBe(2);
    // "api ok" is inferred from the answer arriving at all.
    expect(screen.getByText('api')).toBeInTheDocument();
    expect(screen.getAllByText('ok').length).toBeGreaterThan(0);
    // The footer repeats the version and the node count, still without names.
    expect(screen.getByText('3 nodes')).toBeInTheDocument();
  });

  it('holds the shape of the card with skeleton rows while the status is loading', async () => {
    globalThis.fetch = stubFetch(() => 'hang');

    renderPanel();

    expect(await screen.findByText('Network status')).toBeInTheDocument();
    expect(screen.getByTestId('login-status-skeleton')).toBeInTheDocument();
    expect(screen.queryByText('nodes online')).toBeNull();
  });

  it('says the status is unavailable rather than showing an empty card', async () => {
    globalThis.fetch = stubFetch(() => ({
      status: 503,
      body: { error: { code: 'unavailable', message: 'down' } },
    }));

    renderPanel();

    expect((await screen.findAllByText('status unavailable')).length).toBeGreaterThan(0);
    expect(screen.queryByTestId('login-status-skeleton')).toBeNull();
    expect(screen.queryByText('nodes online')).toBeNull();
  });
});
