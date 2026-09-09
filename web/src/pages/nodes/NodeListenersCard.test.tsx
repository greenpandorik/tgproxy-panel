import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { useNodeRegistrationSecret, usePatchNode } from '@/api/nodes';

import { NodeListenersCard } from './NodeListenersCard';

import type * as NodesApi from '@/api/nodes';
import type { Node } from '@/api/types';

vi.mock('@/api/nodes', async (importOriginal) => ({
  ...(await importOriginal<typeof NodesApi>()),
  usePatchNode: vi.fn(),
  useNodeRegistrationSecret: vi.fn(),
}));

function wrap(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return <QueryClientProvider client={qc}>{node}</QueryClientProvider>;
}

const node = {
  id: '11111111-1111-4111-8111-111111111111',
  name: 'n1',
  hostname: 'n1.test',
  // The address a NAT host's installer detected: the provider's egress, not the interface.
  public_ip: '104.239.66.187',
  acme_email: 'a@b.co',
  status: 'online',
  online: true,
  engine: 'telemt',
  tls_domain: 'n1.test',
  classic_port: 8443,
  ad_tag: '',
  telemt_version: '3.5.5',
  tproxy_version: '',
  agent_version: '1',
  max_profiles: 128,
  profile_count: 0,
  dirty: false,
  last_seen_at: null,
  last_apply_at: null,
  created_at: '2026-09-06T00:00:00Z',
  last_check: null,
} as unknown as Node;

describe('NodeListenersCard', () => {
  const mutateAsync = vi.fn();

  beforeEach(() => {
    setLang('en');
    mutateAsync.mockReset().mockResolvedValue(node);
    vi.mocked(usePatchNode).mockReturnValue({ mutateAsync } as unknown as ReturnType<typeof usePatchNode>);
    vi.mocked(useNodeRegistrationSecret).mockReturnValue({
      data: { secret: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' },
    } as unknown as ReturnType<typeof useNodeRegistrationSecret>);
  });

  it('shows the public IP next to the Fake-TLS fields, prefilled', () => {
    render(wrap(<NodeListenersCard node={node} canEdit />));
    expect(screen.getByText('Addresses and Fake-TLS')).toBeInTheDocument();
    expect(screen.getByLabelText('Fake-TLS domain')).toHaveValue('n1.test');
    expect(screen.getByLabelText('Fake-TLS port')).toHaveValue(8443);
    expect(screen.getByLabelText('Public IP')).toHaveValue('104.239.66.187');
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  });

  it('rejects anything but an IPv4 address before asking to confirm', async () => {
    render(wrap(<NodeListenersCard node={node} canEdit />));
    const ip = screen.getByLabelText('Public IP');
    await userEvent.clear(ip);
    await userEvent.type(ip, '104.239.66.129:443');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('IPv4 address, e.g. 203.0.113.10')).toBeInTheDocument();
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(mutateAsync).not.toHaveBeenCalled();
  });

  // Correcting the address is the NAT fix: the change reaches the node with the next
  // apply. It restarts telemt but does not invalidate links, so it gets its own, lighter
  // confirmation rather than the "reissue every Fake-TLS link" one.
  it('saves a corrected public IP after its own confirmation, sending all three fields', async () => {
    render(wrap(<NodeListenersCard node={node} canEdit />));
    const ip = screen.getByLabelText('Public IP');
    await userEvent.clear(ip);
    await userEvent.type(ip, ' 104.239.66.129 ');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));

    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('Change the public IP?')).toBeInTheDocument();
    expect(within(dialog).queryByText('Change the Fake-TLS settings?')).toBeNull();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(mutateAsync).toHaveBeenCalledTimes(1));
    expect(mutateAsync).toHaveBeenCalledWith({
      tls_domain: 'n1.test',
      classic_port: 8443,
      public_ip: '104.239.66.129',
      ad_tag: '',
    });
  });

  it('keeps the link-reissue confirmation for a Fake-TLS change', async () => {
    render(wrap(<NodeListenersCard node={node} canEdit />));
    const port = screen.getByLabelText('Fake-TLS port');
    await userEvent.clear(port);
    await userEvent.type(port, '9443');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('Change the Fake-TLS settings?')).toBeInTheDocument();
  });

  it('lists the public IP read-only for viewers', () => {
    render(wrap(<NodeListenersCard node={node} canEdit={false} />));
    expect(screen.getByText('Public IP')).toBeInTheDocument();
    expect(screen.getByText('104.239.66.187')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
  });

  it('rejects anything but a 32-character hex tag before asking to confirm', async () => {
    render(wrap(<NodeListenersCard node={node} canEdit />));
    const tag = screen.getByLabelText('Sponsor channel tag');
    await userEvent.type(tag, 'not-a-tag');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('32 lowercase characters (0-9, a-f), or empty')).toBeInTheDocument();
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(mutateAsync).not.toHaveBeenCalled();
  });

  it('saves a sponsor tag after the lighter, non-destructive confirmation', async () => {
    render(wrap(<NodeListenersCard node={node} canEdit />));
    const tag = screen.getByLabelText('Sponsor channel tag');
    await userEvent.type(tag, 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));

    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).queryByText('Change the Fake-TLS settings?')).toBeNull();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(mutateAsync).toHaveBeenCalledTimes(1));
    expect(mutateAsync).toHaveBeenCalledWith({
      tls_domain: 'n1.test',
      classic_port: 8443,
      public_ip: '104.239.66.187',
      ad_tag: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    });
  });

  it('offers a link to @MTProxybot and a way to copy what it needs', () => {
    render(wrap(<NodeListenersCard node={node} canEdit />));
    expect(screen.getByRole('link', { name: /Open @MTProxybot/ })).toHaveAttribute('href', 'https://t.me/MTProxybot');
    expect(screen.getByRole('button', { name: 'Copy 104.239.66.187:8443 for the bot' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Copy the secret for the bot' })).toBeInTheDocument();
  });

  it('has nothing to copy the secret with while it is still loading', () => {
    vi.mocked(useNodeRegistrationSecret).mockReturnValue({
      data: undefined,
    } as unknown as ReturnType<typeof useNodeRegistrationSecret>);
    render(wrap(<NodeListenersCard node={node} canEdit />));
    expect(screen.queryByRole('button', { name: 'Copy the secret for the bot' })).toBeNull();
  });
});
