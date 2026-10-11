import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import '@/i18n';
import { setLang } from '@/i18n';
import { useAuth } from '@/auth/AuthProvider';
import { WebPolicyCard } from './WebPolicyCard';
vi.mock('@/auth/AuthProvider', () => ({ useAuth: vi.fn() }));

const policy = {
  preset: 'automatic',
  carrier: 'https',
  carriers: ['websocket', 'https-lanes'],
  carrier_learning: true,
  carrier_negotiation_aggressiveness: 'conservative',
  overload: { preset: 'balanced', connection_capacity_action: 'wait' },
  timeouts: {
    carrier_negotiation_deadlines_secs: [3, 5, 8, 12],
    carrier_health_secs: 30,
    carrier_learning_secs: 600,
    bridge_request_secs: 10,
    bridge_retry_secs: 90,
    carrier_probe_coalesce_ms: 0,
  },
};
let submitted: Record<string, unknown> | undefined;
async function form(advanced = true, initial: Record<string, unknown> = {}) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') submitted = JSON.parse(String(init.body));
      const data =
        init?.method === 'POST'
          ? { queued: true }
          : {
              policy: { ...policy, ...initial },
              default: policy,
              overridden: false,
              applied_base_path: '',
              advanced_supported: advanced,
            };
      return new Response(JSON.stringify(data), { status: 200, headers: { 'content-type': 'application/json' } });
    }),
  );
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <WebPolicyCard nodeId="fixture" capability="supported" />
    </QueryClientProvider>,
  );
  const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: 'Advanced transport settings' }));
  return user;
}
beforeEach(() => {
  setLang('en');
  submitted = undefined;
  vi.mocked(useAuth).mockReturnValue({ isWriter: true } as ReturnType<typeof useAuth>);
});
it('lets an operator configure an explicit path while showing the currently applied path', async () => {
  const user = await form();
  await user.selectOptions(screen.getByLabelText('WEB path mode'), 'prefix');
  await user.type(screen.getByLabelText('WEB base path'), 'relay/telegram');
  expect(screen.getByText('Applied WEB path: /')).toBeVisible();
  expect(screen.getByText(/New links will be issued only after/)).toBeVisible();
  await user.click(screen.getByRole('button', { name: 'Save and apply' }));
  await waitFor(() => expect(submitted).toMatchObject({ base_path: 'relay/telegram' }));
});
it('preserves independently configured path and HTTP options when changing carrier preset', async () => {
  const user = await form(true, { base_path: 'edge', carrier_method: 'put', conveyor: true, decoy_resolve: 'startup' });
  await user.click(screen.getByRole('button', { name: 'HTTPS only' }));
  await user.click(screen.getByRole('button', { name: 'Save and apply' }));
  await waitFor(() =>
    expect(submitted).toMatchObject({
      base_path: 'edge',
      carrier_method: 'put',
      conveyor: true,
      decoy_resolve: 'startup',
      carriers: false,
    }),
  );
});
it('keeps omitted advanced settings unmanaged when editing legacy carrier settings', async () => {
  const user = await form();
  await user.click(screen.getByRole('button', { name: 'HTTPS only' }));
  await user.click(screen.getByRole('button', { name: 'Save and apply' }));
  await waitFor(() => expect(submitted?.carriers).toBe(false));
  expect(submitted).not.toHaveProperty('carrier_method');
  expect(submitted).not.toHaveProperty('conveyor');
  expect(submitted).not.toHaveProperty('base_path');
  expect(submitted).not.toHaveProperty('decoy_resolve');
});
it('disables new options on an unsupported agent or engine without disabling legacy settings', async () => {
  await form(false);
  expect(screen.getByLabelText('HTTP method')).toBeDisabled();
  expect(screen.getByLabelText('WEB path mode')).toBeDisabled();
  expect(screen.getByLabelText('Learning policy')).toBeEnabled();
});
it('prevents viewer changes to advanced options', async () => {
  vi.mocked(useAuth).mockReturnValue({ isWriter: false } as ReturnType<typeof useAuth>);
  await form();
  expect(screen.getByLabelText('WEB path mode')).toBeDisabled();
  expect(screen.queryByRole('button', { name: 'Save and apply' })).not.toBeInTheDocument();
});
