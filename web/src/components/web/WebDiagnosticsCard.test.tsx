import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { useNodeDiagnostics, useRunWebDiagnostics } from '@/api/web';
import { useAuth } from '@/auth/AuthProvider';

import { WebDiagnosticsCard } from './WebDiagnosticsCard';

import type { DiagnosticGroup, DiagnosticsRun } from '@/api/types';

vi.mock('@/api/web', () => ({ useNodeDiagnostics: vi.fn(), useRunWebDiagnostics: vi.fn() }));
vi.mock('@/auth/AuthProvider', () => ({ useAuth: vi.fn() }));
vi.mock('@/api/nodes', () => ({ useNode: () => ({ data: { tls_domain: 'ams1.example.com' } }) }));

const check = (key: string, status: 'ok' | 'warn' | 'fail' | 'not_available', detail: string) => ({
  key,
  status,
  value: '1',
  detail,
});

function renderWith(groups: DiagnosticGroup[]) {
  const run: DiagnosticsRun = {
    id: 1,
    node_id: 'n1',
    started_at: '2026-09-28T10:00:00Z',
    finished_at: '2026-09-28T10:00:05Z',
    overall_status: 'healthy',
    trigger: 'manual',
    groups,
    passed: 0,
    total: 0,
    not_run: 0,
  };
  vi.mocked(useNodeDiagnostics).mockReturnValue({
    data: { items: [run] },
    isLoading: false,
    isError: false,
  } as unknown as ReturnType<typeof useNodeDiagnostics>);
  vi.mocked(useRunWebDiagnostics).mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false } as unknown as ReturnType<
    typeof useRunWebDiagnostics
  >);
  render(<WebDiagnosticsCard nodeId="n1" />);
}

describe('WebDiagnosticsCard', () => {
  beforeEach(() => {
    setLang('en');
    vi.mocked(useAuth).mockReturnValue({ isWriter: true } as unknown as ReturnType<typeof useAuth>);
  });

  it('leads with what needs attention and keeps passing checks behind a toggle', async () => {
    renderWith([
      {
        key: 'telemt',
        checks: [
          check('telemt_service', 'ok', 'telemt is running on the node'),
          check('tls_front_errors', 'warn', 'worth reading (timeout: 300)'),
        ],
      },
      {
        key: 'web',
        checks: [
          check('http2', 'ok', 'the front negotiates HTTP/2'),
          check('carrier_selections', 'not_available', 'no family yet'),
        ],
      },
    ]);

    expect(screen.getByTestId('diagnostics-summary')).toHaveTextContent('Needs attention: 1');
    const problems = screen.getByTestId('diagnostics-problems');
    expect(within(problems).getAllByRole('listitem')).toHaveLength(1);
    expect(within(problems).getByText(/worth reading \(timeout: 300\)/)).toBeInTheDocument();

    const passed = document.querySelector('[data-check="telemt_service"]');
    expect(passed).not.toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Passed checks: 2' }));
    expect(passed).toBeVisible();
    expect(screen.getByText('telemt is running on the node')).toBeVisible();

    expect(document.querySelector('[data-check="carrier_selections"]')).not.toBeVisible();
    expect(screen.getByRole('button', { name: 'Not run: 1' })).toBeInTheDocument();
  });

  it('says so plainly when everything passed and lists no problems', () => {
    renderWith([{ key: 'telemt', checks: [check('telemt_service', 'ok', 'running'), check('api', 'ok', 'answers')] }]);

    expect(screen.getByTestId('diagnostics-summary')).toHaveTextContent('All clear: 2 of 2 checks passed');
    expect(screen.queryByTestId('diagnostics-problems')).toBeNull();
    expect(screen.queryByText('running')).not.toBeVisible();
  });
});
