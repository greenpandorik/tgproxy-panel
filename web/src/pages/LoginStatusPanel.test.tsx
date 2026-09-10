import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { ThemeProvider } from '@/theme/ThemeProvider';

import { LoginStatusPanel } from './LoginStatusPanel';

function renderPanel(branding: Record<string, unknown> = { panel_name: 'Panel' }) {
  const calls: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      calls.push(String(input));
      return Promise.resolve(
        new Response(JSON.stringify(branding), { status: 200, headers: { 'content-type': 'application/json' } }),
      );
    }) as typeof fetch,
  );
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <ThemeProvider>
        <LoginStatusPanel />
      </ThemeProvider>
    </QueryClientProvider>,
  );
  return calls;
}

describe('LoginStatusPanel', () => {
  beforeEach(() => {
    setLang('en');
    vi.unstubAllGlobals();
  });

  it('shows the operator mark', async () => {
    renderPanel({ panel_name: 'Acme proxies' });
    const mark = await screen.findByTestId('login-brand-mark');
    await waitFor(() => expect(mark).toHaveTextContent('Acme proxies'));
  });

  it('shows the uploaded logo instead of the built-in mark when there is one', async () => {
    renderPanel({ panel_name: 'Acme proxies', logo_url: '/api/v1/branding/assets/x/logo.png?v=1' });
    const mark = await screen.findByTestId('login-brand-mark');
    await waitFor(() => expect(mark.querySelector('img')).toHaveAttribute('src', '/api/v1/branding/assets/x/logo.png?v=1'));
  });

  /*
   * The login page answers anyone who reaches the address. The size of the fleet, how much of
   * it is up and which version is running are facts about the deployment, useful to whoever is
   * probing it and useless to the operator, who owns it and can read them once inside.
   */
  it('says nothing about the fleet, and does not even ask', async () => {
    const calls = renderPanel({ panel_name: 'Acme proxies' });
    await screen.findByTestId('login-brand-mark');

    const panel = screen.getByTestId('login-status-panel');
    expect(panel.textContent).not.toMatch(/\d+\s*\/\s*\d+/);
    expect(panel.textContent).not.toMatch(/v?\d+\.\d+\.\d+/);
    expect(calls.some((c) => c.includes('/status'))).toBe(false);
  });
});
