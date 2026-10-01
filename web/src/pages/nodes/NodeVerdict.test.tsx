import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it } from 'vitest';

import i18n, { setLang } from '@/i18n';

import { checkTally } from './nodeHealth';
import { NodeVerdict } from './NodeVerdict';
import { nodeVerdict } from './serverVerdict';

import type { Alert, DiagnosticsRun, Node } from '@/api/types';

const node = (over: Partial<Node> = {}) =>
  ({ id: 'n1', name: 'Test2', engine: 'telemt', status: 'online', online: true, last_seen_at: new Date().toISOString(), ...over }) as Node;

const alert = (over: Partial<Alert>) => ({ id: 1, created_at: new Date().toISOString(), message: '', ...over }) as Alert;

const run = (statuses: string[]) =>
  ({
    id: 1,
    started_at: new Date().toISOString(),
    finished_at: new Date().toISOString(),
    groups: [{ key: 'telemt', checks: statuses.map((status, i) => ({ key: `c${i}`, status, value: null, detail: null })) }],
  }) as unknown as DiagnosticsRun;

const verdictOf = (n: Node, alerts: Alert[] = [], statuses: string[] = []) =>
  nodeVerdict(n, alerts, checkTally(run(statuses), null), i18n.t, i18n);

describe('nodeVerdict', () => {
  beforeEach(() => setLang('ru'));

  it('says the node is not connected before anything else', () => {
    const v = verdictOf(node({ online: false, status: 'offline' }), [alert({ node_id: 'n1', kind: 'node_offline' })]);
    expect(v.tone).toBe('err');
    expect(v.headline).toBe('Сервер не на связи');
  });

  it('lists this node’s open incidents by name, not by the server’s log line', () => {
    const v = verdictOf(node(), [
      alert({ node_id: 'n1', kind: 'diagnostic_telemt_tls_front_errors', message: 'Scheduled check: telemt / tls_front_errors: …' }),
      alert({ node_id: 'other', kind: 'node_offline' }),
    ]);
    expect(v.headline).toBe('Требует внимания: 1');
    expect(v.lines).toEqual(['Диагностика: Сбои TLS-рукопожатий']);
  });

  it('counts failed checks when nothing is open, and is fine when they all passed', () => {
    expect(verdictOf(node(), [], ['ok', 'fail']).tone).toBe('warn');
    expect(verdictOf(node(), [], ['ok', 'ok']).tone).toBe('ok');
  });
});

describe('NodeVerdict', () => {
  beforeEach(() => setLang('ru'));

  it('shows nothing while all is well', () => {
    const { container } = render(<NodeVerdict verdict={{ tone: 'ok', headline: 'Всё в порядке', lines: [] }} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('gives an offline server the commands to run and a way to the install command', () => {
    render(
      <MemoryRouter>
        <NodeVerdict verdict={verdictOf(node({ online: false, status: 'offline' }))} />
      </MemoryRouter>,
    );
    expect(screen.getByTestId('node-verdict')).toHaveAttribute('data-tone', 'err');
    expect(screen.getByText('systemctl status tgwp-agent')).toBeInTheDocument();
    expect(screen.getByText('Открыть обслуживание').closest('a')).toHaveAttribute('href', '/?section=settings');
  });
});
