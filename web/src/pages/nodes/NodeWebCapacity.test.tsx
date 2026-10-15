import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { usePatchNode } from '@/api/nodes';

import { NodeWebCapacity } from './NodeWebCapacity';

import type * as NodesApi from '@/api/nodes';
import type { Node } from '@/api/types';

vi.mock('@/api/nodes', async (importOriginal) => ({
  ...(await importOriginal<typeof NodesApi>()),
  usePatchNode: vi.fn(),
}));

function wrap(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return <QueryClientProvider client={qc}>{node}</QueryClientProvider>;
}

const base = {
  id: '11111111-1111-4111-8111-111111111111',
  name: 'n1',
  hostname: 'n1.test',
  engine: 'telemt',
  online: true,
  agent_version: '2.17.4',
  telemt_version: '3.5.14',
  web_capacity: null,
  web_capacity_supported: true,
  health: { web_capacity: 128, max_connections: 10000 },
} as unknown as Node;

describe('NodeWebCapacity', () => {
  const mutateAsync = vi.fn();

  beforeEach(() => {
    setLang('ru');
    mutateAsync.mockReset().mockResolvedValue(base);
    vi.mocked(usePatchNode).mockReturnValue({ mutateAsync, isPending: false } as unknown as ReturnType<typeof usePatchNode>);
  });

  it('offers the four presets with both ceilings and shows what the server runs now', () => {
    render(wrap(<NodeWebCapacity node={base} canEdit />));
    const select = screen.getByLabelText('Ёмкость WEB');
    expect(select).toHaveValue('128');
    const options = within(select).getAllByRole('option');
    expect(options.map((o) => o.getAttribute('value'))).toEqual(['128', '256', '512', '1024']);
    expect(options[3].textContent?.replace(/\s/g, ' ')).toBe('до 1024 человек по WEB и до 40 000 соединений Fake-TLS');
    expect(options[0].textContent).toContain('стандарт');
    expect(screen.getByTestId('web-capacity-current').textContent?.replace(/\s/g, ' ')).toBe(
      'Сейчас на сервере: до 128 человек по WEB и до 10 000 соединений Fake-TLS.',
    );
    expect(
      screen.getByText('При смене telemt перезапустится, подключённые люди переподключатся за несколько секунд.'),
    ).toBeInTheDocument();
    expect(screen.getByText(/не меньше 4 ГБ RAM/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeDisabled();
  });

  it('saves a new preset after confirming the restart', async () => {
    render(wrap(<NodeWebCapacity node={base} canEdit />));
    await userEvent.selectOptions(screen.getByLabelText('Ёмкость WEB'), '1024');
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('Сменить ёмкость WEB?')).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(mutateAsync).toHaveBeenCalledWith({ web_capacity: 1024 }));
  });

  it('starts from the stored choice when there is one', () => {
    render(wrap(<NodeWebCapacity node={{ ...base, web_capacity: 512 }} canEdit />));
    expect(screen.getByLabelText('Ёмкость WEB')).toHaveValue('512');
  });

  it('locks the choice and names the versions it needs on an older agent', () => {
    const old = { ...base, agent_version: '2.17.3', web_capacity_supported: false, health: undefined } as unknown as Node;
    render(wrap(<NodeWebCapacity node={old} canEdit />));
    expect(screen.getByLabelText('Ёмкость WEB')).toBeDisabled();
    expect(screen.getByTestId('web-capacity-unsupported').textContent).toContain('агентом 2.17.4 или новее');
    expect(screen.getByTestId('web-capacity-current').textContent).toContain('ещё не сообщил');
    expect(screen.queryByRole('button', { name: 'Сохранить' })).toBeNull();
  });

  it('is read-only for viewers', () => {
    setLang('en');
    render(wrap(<NodeWebCapacity node={base} canEdit={false} />));
    expect(screen.getByLabelText('WEB capacity')).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
    expect(screen.queryByTestId('web-capacity-unsupported')).toBeNull();
  });
});
