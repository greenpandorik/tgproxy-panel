import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { useAuth } from '@/auth/AuthProvider';

import { NodeCheckCard } from './NodeCheckCard';

import type { Node, NodeCheckReport } from '@/api/types';

vi.mock('@/auth/AuthProvider', () => ({ useAuth: vi.fn() }));

function wrap(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return <QueryClientProvider client={qc}>{node}</QueryClientProvider>;
}

function nodeWith(report: NodeCheckReport): Node {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    name: 'n1',
    hostname: 'n1.test',
    public_ip: '5.6.7.8',
    acme_email: 'a@b.co',
    status: 'online',
    online: true,
    engine: 'telemt',
    tls_domain: 'n1.test',
    classic_port: 8443,
    telemt_version: '3.5.5',
    tproxy_version: '',
    agent_version: '1',
    max_profiles: 128,
    profile_count: 0,
    dirty: false,
    last_seen_at: null,
    last_apply_at: null,
    created_at: '2026-09-06T00:00:00Z',
    last_check: report,
  } as Node;
}

describe('NodeCheckCard', () => {
  beforeEach(() => {
    vi.mocked(useAuth).mockReturnValue({ isWriter: false } as unknown as ReturnType<typeof useAuth>);
    setLang('en');
  });

  // Task 46b: pq_kex is informational. A front that only negotiates classical key exchange
  // must not read as a failed prerequisite - neutral dot, neutral detail, roll-up still green.
  it('renders a failing advisory probe in the info tone and keeps the roll-up green', () => {
    render(
      wrap(
        <NodeCheckCard
          node={nodeWith({
            ran_at: '2026-09-06T00:00:00Z',
            all_ok: true,
            results: [
              { name: 'tls_cert', ok: true, detail: 'CN=n1.test' },
              { name: 'pq_kex', ok: false, advisory: true, detail: 'server chose X25519 (no post-quantum key exchange)' },
              { name: 'http_root', ok: false, detail: 'status=502 bytes=0' },
            ],
          })}
        />,
      ),
    );
    expect(screen.getByText('All checks passed')).toBeInTheDocument();
    expect(screen.getByText('Post-quantum key exchange')).toBeInTheDocument();
    expect(screen.getByText(/Informational/)).toBeInTheDocument();

    const pqDetail = screen.getByText('server chose X25519 (no post-quantum key exchange)');
    expect(pqDetail.className).toContain('text-info');
    expect(pqDetail.className).not.toContain('text-err');
    const pqRow = pqDetail.closest('li');
    expect(pqRow?.querySelector('.bg-info')).not.toBeNull();
    expect(pqRow?.querySelector('.bg-err')).toBeNull();

    // A hard failure still reads red.
    const httpDetail = screen.getByText('status=502 bytes=0');
    expect(httpDetail.className).toContain('text-err');
    expect(httpDetail.closest('li')?.querySelector('.bg-err')).not.toBeNull();
  });

  it('renders a passing advisory probe like any other pass', () => {
    render(
      wrap(
        <NodeCheckCard
          node={nodeWith({
            ran_at: '2026-09-06T00:00:00Z',
            all_ok: true,
            results: [{ name: 'pq_kex', ok: true, advisory: true, detail: 'X25519MLKEM768 negotiated' }],
          })}
        />,
      ),
    );
    const detail = screen.getByText('X25519MLKEM768 negotiated');
    expect(detail.className).toContain('text-mute');
    expect(detail.closest('li')?.querySelector('.bg-ok')).not.toBeNull();
  });
});
