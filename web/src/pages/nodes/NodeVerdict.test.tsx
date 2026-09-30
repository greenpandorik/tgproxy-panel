import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { useAlerts } from '@/api/dashboard';
import { useNodeDiagnostics } from '@/api/web';

import { NodeVerdict } from './NodeVerdict';

import type { Node } from '@/api/types';

vi.mock('@/api/dashboard', () => ({ useAlerts: vi.fn() }));
vi.mock('@/api/web', () => ({ useNodeDiagnostics: vi.fn() }));

const node = (over: Partial<Node> = {}) =>
  ({
    id: 'n1',
    name: 'Test2',
    engine: 'telemt',
    status: 'online',
    online: true,
    last_seen_at: new Date().toISOString(),
    ...over,
  }) as Node;

function setup(alerts: { node_id?: string; kind: string; message: string }[], checks: { key: string; status: string }[] = []) {
  vi.mocked(useAlerts).mockReturnValue({
    data: { items: alerts.map((a, i) => ({ id: i, created_at: new Date().toISOString(), ...a })) },
  } as unknown as ReturnType<typeof useAlerts>);
  vi.mocked(useNodeDiagnostics).mockReturnValue({
    data: {
      items: [
        {
          id: 1,
          finished_at: new Date().toISOString(),
          started_at: new Date().toISOString(),
          groups: [{ key: 'telemt', checks }],
        },
      ],
    },
  } as unknown as ReturnType<typeof useNodeDiagnostics>);
}

const renderVerdict = (n: Node) =>
  render(
    <MemoryRouter>
      <NodeVerdict node={n} />
    </MemoryRouter>,
  );

describe('NodeVerdict', () => {
  beforeEach(() => setLang('ru'));

  it('says the node is not connected before anything else', () => {
    setup([{ node_id: 'n1', kind: 'node_offline', message: '' }]);
    renderVerdict(node({ online: false, status: 'offline' }));
    expect(screen.getByTestId('node-verdict')).toHaveAttribute('data-tone', 'err');
    expect(screen.getByText('Сервер не на связи')).toBeInTheDocument();
  });

  it('lists this node’s open incidents by name, not by the server’s log line', () => {
    setup([
      { node_id: 'n1', kind: 'diagnostic_telemt_tls_front_errors', message: 'Scheduled check: telemt / tls_front_errors: …' },
      { node_id: 'other', kind: 'node_offline', message: '' },
    ]);
    renderVerdict(node());
    expect(screen.getByText('Требует внимания: 1')).toBeInTheDocument();
    expect(screen.getByText('Диагностика: Сбои TLS-рукопожатий')).toBeInTheDocument();
    expect(screen.queryByText(/Scheduled check/)).toBeNull();
    expect(screen.getByText('Открыть проверки').closest('a')).toHaveAttribute('href', '/?section=diagnostics');
  });

  it('says all is well when nothing is open and the last diagnostics were clean', () => {
    setup([], [{ key: 'telemt_service', status: 'ok' }]);
    renderVerdict(node());
    expect(screen.getByTestId('node-verdict')).toHaveAttribute('data-tone', 'ok');
    expect(screen.getByText('Всё в порядке')).toBeInTheDocument();
  });
});
