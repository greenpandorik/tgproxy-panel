import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import type { ApiToken } from '@/api/apiTokens';
import { AuthProvider } from '@/auth/AuthProvider';
import { ApiTokensPanel } from './ApiTokensPanel';

const TOKEN: ApiToken = {
  id: 't1',
  name: 'Service Bot',
  prefix: 'tgwp_public',
  scopes: ['nodes:read'],
  created_at: '2026-10-01T10:00:00Z',
  expires_at: '2099-10-01T10:00:00Z',
  last_used_at: null,
  revoked_at: null,
};
const SECRET = 'tgwp_one_time_secret_only';
const SCOPES = [
  { id: 'nodes:read', resource: 'nodes', action: 'read' },
  { id: 'nodes:write', resource: 'nodes', action: 'write' },
  { id: 'users:read', resource: 'users', action: 'read' },
  { id: 'users:write', resource: 'users', action: 'write' },
  { id: 'audit:read', resource: 'audit', action: 'read' },
];
function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}
let role = 'owner';
let records: ApiToken[] = [TOKEN];
let createdBody: unknown;
let revoked = false;
let createError = false;
function mount(qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  const view = render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AuthProvider>
          <ApiTokensPanel />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { qc, ...view };
}
async function openCreate() {
  await waitFor(() => expect(screen.getByRole('button', { name: 'Create token' })).toBeEnabled());
  await userEvent.click(screen.getByRole('button', { name: 'Create token' }));
  return within(screen.getByRole('dialog', { name: 'Create API token' }));
}
async function submitCreate() {
  const dialog = await openCreate();
  await userEvent.type(dialog.getByLabelText('Name'), '  New Bot  ');
  await userEvent.click(dialog.getByRole('button', { name: 'Create token' }));
  return within(await screen.findByRole('dialog', { name: 'Save your API token' }));
}
describe('ApiTokensPanel', () => {
  beforeEach(() => {
    setLang('en');
    document.cookie = 'tgwp_csrf=test-csrf';
    role = 'owner';
    records = [TOKEN];
    createdBody = undefined;
    revoked = false;
    createError = false;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path.endsWith('/auth/me'))
          return json({ id: 'a1', username: 'root', role, totp_enabled: false, features: { totp: true } });
        if (path.endsWith('/api-tokens/scopes'))
          return json({
            scopes: role === 'viewer' ? SCOPES.filter((s) => s.action === 'read') : SCOPES,
            max_expires_in_days: 365,
            max_tokens: 50,
          });
        if (path.endsWith('/api-tokens') && init?.method === 'POST') {
          createdBody = JSON.parse(String(init.body));
          if (createError) return json({ error: { code: 'token_limit', message: 'Too many active tokens' } }, 409);
          records = [...records, { ...TOKEN, id: 't2', name: 'New Bot' }];
          return json({ token: SECRET, api_token: records[1] }, 201);
        }
        if (path.endsWith('/api-tokens/t1') && init?.method === 'DELETE') {
          revoked = true;
          records = [{ ...TOKEN, revoked_at: '2026-10-08T10:00:00Z' }];
          return Promise.resolve(new Response(null, { status: 204 }));
        }
        if (path.endsWith('/api-tokens')) return json({ items: records, total: records.length });
        return json({ error: { code: 'not_found', message: 'Unknown endpoint' } }, 404);
      }),
    );
  });

  it('isolates fresh cached metadata and scope catalogs when the administrator changes', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    const owner = { id: 'a1', username: 'owner-a', role: 'owner', totp_enabled: false, features: { totp: true } };
    const catalog = { scopes: SCOPES, max_expires_in_days: 365, max_tokens: 50 };
    qc.setQueryData(['auth', 'me'], owner);
    qc.setQueryData(['api-tokens', 'list'], { items: [TOKEN], total: 1 });
    qc.setQueryData(['api-tokens', 'scopes'], catalog);
    qc.setQueryData(['api-tokens', 'list', 'a1'], { items: [TOKEN], total: 1 });
    qc.setQueryData(['api-tokens', 'scopes', 'a1'], catalog);
    mount(qc);
    expect(await screen.findByText('Service Bot')).toBeInTheDocument();
    let deliverList: (response: Response) => void = () => {
      throw new Error('B list not requested');
    };
    const requested: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        requested.push(path);
        if (path.endsWith('/api-tokens/scopes')) return json({ scopes: [SCOPES[4]], max_expires_in_days: 365, max_tokens: 50 });
        if (path.endsWith('/api-tokens'))
          return new Promise<Response>((resolve) => {
            deliverList = resolve;
          });
        return json(owner);
      }),
    );
    await act(async () => {
      qc.setQueryData(['auth', 'me'], { ...owner, id: 'b1', username: 'owner-b' });
    });
    await waitFor(() => expect(screen.queryByText('Service Bot')).not.toBeInTheDocument());
    await waitFor(() => expect(requested).toEqual(expect.arrayContaining(['/api/v1/api-tokens', '/api/v1/api-tokens/scopes'])));
    await act(async () => {
      deliverList(
        new Response(JSON.stringify({ items: [{ ...TOKEN, id: 'b-token', name: 'B automation' }], total: 1 }), {
          headers: { 'content-type': 'application/json' },
        }),
      );
    });
    expect(await screen.findByText('B automation')).toBeInTheDocument();
    expect(screen.queryByText('Service Bot')).not.toBeInTheDocument();
    const dialog = await openCreate();
    expect(dialog.getAllByRole('checkbox')).toHaveLength(1);
    expect(dialog.getByText('Audit log')).toBeInTheDocument();
    expect(dialog.queryByText('Servers')).not.toBeInTheDocument();
  });

  it('removes the one-time secret immediately when the administrator changes', async () => {
    const { qc } = mount();
    await submitCreate();
    expect(screen.getByText(SECRET)).toBeInTheDocument();
    await act(async () => {
      qc.setQueryData(['auth', 'me'], {
        id: 'b1',
        username: 'account-b',
        role: 'viewer',
        totp_enabled: false,
        features: { totp: true },
      });
    });
    await waitFor(() => expect(screen.queryByText(SECRET)).not.toBeInTheDocument());
    expect(screen.queryByRole('dialog', { name: 'Save your API token' })).not.toBeInTheDocument();
  });

  it('keeps a pending creation tied to its initiating account after an account switch', async () => {
    const { qc } = mount(new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } }));
    const dialog = await openCreate();
    qc.setQueryData(['api-tokens', 'list', 'a1'], { items: [TOKEN], total: 1 });
    qc.setQueryData(['api-tokens', 'list', 'b1'], { items: [], total: 0 });
    let completeCreation: (response: Response) => void = () => {
      throw new Error('creation not requested');
    };
    let otherAccountListRequests = 0;
    const originalFetch = globalThis.fetch;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        if (String(input).endsWith('/api-tokens') && init?.method === 'POST')
          return new Promise<Response>((resolve) => {
            completeCreation = resolve;
          });
        if (String(input).endsWith('/api-tokens')) otherAccountListRequests += 1;
        return originalFetch(input, init);
      }),
    );
    await userEvent.type(dialog.getByLabelText('Name'), 'Pending Bot');
    await userEvent.click(dialog.getByRole('button', { name: 'Create token' }));
    await act(async () => {
      qc.setQueryData(['auth', 'me'], {
        id: 'b1',
        username: 'account-b',
        role: 'viewer',
        totp_enabled: false,
        features: { totp: true },
      });
    });
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create API token' })).not.toBeInTheDocument());
    await act(async () => {
      completeCreation(
        new Response(JSON.stringify({ token: SECRET, api_token: { ...TOKEN, name: 'Pending Bot' } }), {
          status: 201,
          headers: { 'content-type': 'application/json' },
        }),
      );
    });
    await waitFor(() =>
      expect(
        qc
          .getMutationCache()
          .getAll()
          .every((mutation) => mutation.state.status !== 'pending'),
      ).toBe(true),
    );
    expect(screen.queryByText(SECRET)).not.toBeInTheDocument();
    expect(qc.getQueryState(['api-tokens', 'list', 'a1'])?.isInvalidated).toBe(true);
    expect(qc.getQueryState(['api-tokens', 'list', 'b1'])?.isInvalidated).toBe(false);
    expect(otherAccountListRequests).toBe(0);
  });

  it('does not request personal tokens before the administrator identity is known', async () => {
    const requested: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        requested.push(path);
        if (path.endsWith('/auth/me')) return new Promise<Response>(() => {});
        return json({ items: [TOKEN], total: 1 });
      }),
    );
    mount();
    await act(async () => {
      await Promise.resolve();
    });
    expect(requested).toEqual(['/api/v1/auth/me']);
    expect(screen.queryByText('Service Bot')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Create token' })).toBeDisabled();
  });

  it('lists only public metadata and derives active, expired and revoked states', async () => {
    records = [
      TOKEN,
      { ...TOKEN, id: 'expired', name: 'Old Bot', expires_at: '2020-01-01T00:00:00Z' },
      { ...TOKEN, id: 'revoked', name: 'Revoked Bot', revoked_at: '2026-10-02T00:00:00Z' },
    ];
    mount();
    expect(await screen.findByText('Service Bot')).toBeInTheDocument();
    expect(screen.getByText('Active')).toBeInTheDocument();
    expect(screen.getByText('Expired')).toBeInTheDocument();
    expect(screen.getByText('Revoked')).toBeInTheDocument();
    expect(screen.getAllByText('Never used')).toHaveLength(3);
    expect(screen.getByRole('link', { name: 'API documentation' })).toHaveAttribute('href', 'https://tgproxypanel.com/en/api/');
    expect(screen.queryByText(SECRET)).not.toBeInTheDocument();
  });

  it('creates with trimmed name and read preset; keeps the secret through refresh, copies and clears it on close', async () => {
    const user = userEvent.setup();
    const { qc } = mount();
    const dialog = await submitCreate();
    expect(createdBody).toEqual({ name: 'New Bot', expires_in_days: 30, scopes: ['nodes:read', 'users:read', 'audit:read'] });
    expect(dialog.getByText(SECRET)).toBeInTheDocument();
    expect(dialog.getByText(/shown only once/)).toBeInTheDocument();
    await qc.invalidateQueries({ queryKey: ['api-tokens', 'list'] });
    expect(dialog.getByText(SECRET)).toBeInTheDocument();
    await user.click(dialog.getByRole('button', { name: 'Copy token' }));
    expect(await navigator.clipboard.readText()).toBe(SECRET);
    expect(
      JSON.stringify(
        qc
          .getQueryCache()
          .getAll()
          .map((q) => q.state),
      ),
    ).not.toContain(SECRET);
    expect(
      JSON.stringify(
        qc
          .getMutationCache()
          .getAll()
          .map((m) => m.state.data),
      ),
    ).not.toContain(SECRET);
    await user.click(dialog.getByRole('button', { name: 'Done' }));
    await waitFor(() => expect(screen.queryByText(SECRET)).not.toBeInTheDocument());
    expect(
      JSON.stringify(
        qc
          .getMutationCache()
          .getAll()
          .map((m) => m.state),
      ),
    ).not.toContain(SECRET);
    expect(JSON.stringify(window.localStorage)).not.toContain(SECRET);
    expect(JSON.stringify(window.sessionStorage)).not.toContain(SECRET);
    await openCreate();
    expect(screen.queryByText(SECRET)).not.toBeInTheDocument();
  });

  it('validates Unicode name length, integer lifetime and at least one permission before POST', async () => {
    mount();
    const dialog = await openCreate();
    await userEvent.click(dialog.getByRole('button', { name: 'Create token' }));
    expect(dialog.getByText('Use 1–80 characters.')).toBeInTheDocument();
    await userEvent.type(dialog.getByLabelText('Name'), '🚀'.repeat(80));
    await userEvent.clear(dialog.getByLabelText('Lifetime (days)'));
    await userEvent.type(dialog.getByLabelText('Lifetime (days)'), '366');
    await userEvent.click(dialog.getByRole('button', { name: 'Create token' }));
    expect(dialog.getByText('Use a whole number from 1 to 365.')).toBeInTheDocument();
    expect(dialog.queryByText('Use 1–80 characters.')).not.toBeInTheDocument();
    await userEvent.clear(dialog.getByLabelText('Lifetime (days)'));
    await userEvent.type(dialog.getByLabelText('Lifetime (days)'), '1.5');
    await userEvent.click(dialog.getByRole('button', { name: 'Create token' }));
    expect(dialog.getByText('Use a whole number from 1 to 365.')).toBeInTheDocument();
    await userEvent.clear(dialog.getByLabelText('Lifetime (days)'));
    await userEvent.type(dialog.getByLabelText('Lifetime (days)'), '30');
    for (const checkbox of dialog.getAllByRole('checkbox', { checked: true })) await userEvent.click(checkbox);
    await userEvent.click(dialog.getByRole('button', { name: 'Create token' }));
    expect(dialog.getByText('Select at least one permission.')).toBeInTheDocument();
    expect(createdBody).toBeUndefined();
  });

  it('restricts viewers to read choices while allowing their own token creation', async () => {
    role = 'viewer';
    mount();
    const dialog = await openCreate();
    expect(dialog.getByRole('button', { name: 'Read only' })).toBeInTheDocument();
    expect(dialog.queryByRole('button', { name: 'Full access' })).not.toBeInTheDocument();
    expect(dialog.queryByRole('checkbox', { name: /Write/ })).not.toBeInTheDocument();
    await userEvent.type(dialog.getByLabelText('Name'), 'Reader');
    await userEvent.click(dialog.getByRole('button', { name: 'Create token' }));
    await screen.findByRole('dialog', { name: 'Save your API token' });
    expect(createdBody).toEqual({ name: 'Reader', expires_in_days: 30, scopes: ['nodes:read', 'users:read', 'audit:read'] });
  });

  it('full preset selects only available automation scopes and explains excluded operations', async () => {
    mount();
    const dialog = await openCreate();
    await userEvent.click(dialog.getByRole('button', { name: 'Full access' }));
    expect(dialog.getAllByRole('checkbox', { checked: true })).toHaveLength(5);
    expect(dialog.getByText(/account security, administrators, backups or service credentials/)).toBeInTheDocument();
    await userEvent.click(dialog.getByRole('button', { name: 'Read only' }));
    expect(dialog.getAllByRole('checkbox', { checked: true })).toHaveLength(3);
  });

  it('requires confirmation to revoke and updates the public list after success', async () => {
    mount();
    await userEvent.click(await screen.findByRole('button', { name: 'Revoke Service Bot' }));
    let dialog = within(screen.getByRole('dialog', { name: 'Revoke “Service Bot”?' }));
    expect(revoked).toBe(false);
    await userEvent.click(dialog.getByRole('button', { name: 'Cancel' }));
    expect(revoked).toBe(false);
    await userEvent.click(screen.getByRole('button', { name: 'Revoke Service Bot' }));
    dialog = within(screen.getByRole('dialog', { name: 'Revoke “Service Bot”?' }));
    await userEvent.click(dialog.getByRole('button', { name: 'Revoke token' }));
    expect(await screen.findByText('Revoked')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Revoke Service Bot' })).not.toBeInTheDocument();
  });

  it('keeps creation errors visible without exposing a secret', async () => {
    createError = true;
    mount();
    const dialog = await openCreate();
    await userEvent.type(dialog.getByLabelText('Name'), 'New Bot');
    await userEvent.click(dialog.getByRole('button', { name: 'Create token' }));
    expect(await dialog.findByRole('alert')).toHaveTextContent('Too many active tokens');
    expect(screen.queryByText(SECRET)).not.toBeInTheDocument();
  });
});
