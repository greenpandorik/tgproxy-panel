import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { NodeHealthStrip } from './NodeHealthStrip';
import { dcState } from './nodeHealth';

import type { CheckTally } from './nodeHealth';
import type { Verdict } from './serverVerdict';
import type { Node, NodeHealth } from '@/api/types';

const node = (over: Partial<Node> = {}) =>
  ({
    id: 'n1',
    engine: 'telemt',
    online: true,
    status: 'online',
    last_seen_at: new Date(Date.now() - 4000).toISOString(),
    people_online: 6,
    connections: 41,
    ...over,
  }) as Node;

const health = (over: Partial<NodeHealth> = {}) =>
  ({
    cpu_utilisation_percent: 24,
    mem_used_percent: 38,
    effective_latency_ms: 127,
    upstream_healthy: true,
    dc_data_available: true,
    dcs: [1, 2, 3, 4, 5].map((dc) => ({ dc, latency_ms: 40 + dc, known: true, ip_preference: 'ipv4' })),
    ...over,
  }) as NodeHealth;

const ok: Verdict = { tone: 'ok', headline: '', lines: [] };
const checks: CheckTally = { passed: 12, warned: 0, failed: 0, total: 12, ranAt: new Date(Date.now() - 7200_000).toISOString() };

function strip(n: Node, h: NodeHealth | undefined, verdict = ok, tally = checks) {
  render(
    <MemoryRouter>
      <NodeHealthStrip node={n} verdict={verdict} health={h} loading={false} dcs={dcState(h)} checks={tally} />
    </MemoryRouter>,
  );
  return screen.getByRole('group', { name: 'Сводка по серверу' });
}

const cell = (group: HTMLElement, label: string) => within(group).getByText(label).parentElement as HTMLElement;

describe('NodeHealthStrip', () => {
  beforeEach(() => setLang('ru'));

  it('shows the five key numbers of a working server, people linking to its users', () => {
    const group = strip(node(), health());
    expect(within(cell(group, 'Статус')).getByText('Работает')).toHaveClass('text-ok');
    expect(within(cell(group, 'Статус')).getByText(/^ответ .* назад$/)).toBeInTheDocument();

    const people = within(group).getByRole('link', { name: /Люди онлайн/ });
    expect(people).toHaveAttribute('href', '/users?node=n1');
    expect(people).toHaveTextContent('≈ 6');
    expect(people).toHaveTextContent('соединений: 41');

    expect(cell(group, 'Нагрузка')).toHaveTextContent('24% · 38%');
    expect(cell(group, 'Нагрузка')).toHaveTextContent('процессор · память');
    expect(cell(group, 'До Telegram')).toHaveTextContent('127 мс');
    expect(cell(group, 'До Telegram')).toHaveTextContent('5 из 5 датацентров');
    expect(cell(group, 'Проверка')).toHaveTextContent('12 из 12');
    expect(cell(group, 'Проверка')).toHaveTextContent('2 ч назад');
  });

  it('colours only the figures that are a problem', () => {
    const group = strip(
      node(),
      health({ cpu_utilisation_percent: 97 }),
      { tone: 'warn', headline: '', lines: [] },
      {
        ...checks,
        passed: 11,
        failed: 1,
      },
    );
    expect(within(cell(group, 'Статус')).getByText('Есть проблемы')).toHaveClass('text-warn');
    expect(within(cell(group, 'Нагрузка')).getByText(/97%/)).toHaveClass('text-err');
    expect(within(cell(group, 'Проверка')).getByText('11 из 12')).toHaveClass('text-err');
    expect(within(cell(group, 'До Telegram')).getByText('127 мс')).not.toHaveClass('text-ok');
  });

  it('says an offline server is not connected and shows no figures it cannot vouch for', () => {
    const group = strip(
      node({ online: false, status: 'offline', people_online: null, connections: null }),
      undefined,
      { tone: 'err', headline: '', lines: [] },
      { passed: 0, warned: 0, failed: 0, total: 0, ranAt: null },
    );
    expect(within(cell(group, 'Статус')).getByText('Не на связи')).toHaveClass('text-err');
    expect(within(cell(group, 'Нагрузка')).getAllByText('Нет данных')).toHaveLength(2);
    expect(cell(group, 'Проверка')).toHaveTextContent('не проводилась');
  });

  it('has no Telegram figure on a tproxy server', () => {
    const group = strip(node({ engine: 'tproxy' }), health());
    expect(within(group).queryByText('До Telegram')).toBeNull();
  });
});
