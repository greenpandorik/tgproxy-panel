import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import '@/i18n';
import { setLang } from '@/i18n';
import { useReliability, useSaveReliability } from '@/api/reliability';
import type { RecoveryState } from '@/api/reliability';
import type { Node } from '@/api/types';
import { NodeReliability } from './NodeReliability';
vi.mock('@/auth/AuthProvider', () => ({ useAuth: () => ({ isWriter: true }) }));
vi.mock('@/api/reliability', () => ({ useReliability: vi.fn(), useSaveReliability: vi.fn() }));
const mutate = vi.fn();
function renderPolicy(egress: 'direct' | 'unmanaged', active: string) {
  const state: RecoveryState = {
    policy: {
      egress,
      recovery: 'observe',
      maintenance: false,
      failure_threshold: 3,
      cooldown_seconds: 300,
      max_actions_hour: 2,
      socks_address: '',
      reserve_socks_address: '127.0.0.1:1081',
      automatic_failover: egress === 'direct',
    },
    active,
    events: [],
    last: '',
  };
  vi.mocked(useReliability).mockReturnValue({ data: state, isLoading: false, isError: false } as ReturnType<
    typeof useReliability
  >);
  vi.mocked(useSaveReliability).mockReturnValue({ mutate, isPending: false, isError: false } as unknown as ReturnType<
    typeof useSaveReliability
  >);
  render(<NodeReliability node={{ id: 'fixture', online: true } as Node} />);
}
beforeEach(() => {
  setLang('en');
  mutate.mockClear();
});
it('does not offer primary restoration for an unmanaged route', () => {
  renderPolicy('unmanaged', 'unmanaged');
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  expect(screen.queryByRole('button', { name: 'Check and restore primary route' })).not.toBeInTheDocument();
});
it('clears automatic failover when handing a direct route back to manual control', async () => {
  const user = userEvent.setup();
  renderPolicy('direct', 'direct');
  await user.selectOptions(screen.getAllByRole('combobox')[1], 'unmanaged');
  await user.click(screen.getByRole('button', { name: 'Save' }));
  expect(mutate).toHaveBeenCalledWith(
    expect.objectContaining({ egress: 'unmanaged', automatic_failover: false, restore_primary: false }),
    expect.anything(),
  );
});
it('does not request a route change when enabling maintenance on a reserve route', async () => {
  const user = userEvent.setup();
  renderPolicy('direct', '127.0.0.1:1081');
  expect(screen.getByRole('button', { name: 'Check and restore primary route' })).toBeEnabled();
  await user.click(screen.getByRole('checkbox', { name: /Maintenance/ }));
  await user.click(screen.getByRole('button', { name: 'Save' }));
  expect(mutate).toHaveBeenCalledWith(expect.objectContaining({ maintenance: true, restore_primary: false }), expect.anything());
});
