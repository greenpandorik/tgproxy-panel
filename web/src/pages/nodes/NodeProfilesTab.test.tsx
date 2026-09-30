import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { useNodeProfiles } from '@/api/nodes';
import { ApiError } from '@/lib/api';

import { NodeProfilesTab } from './NodeProfilesTab';

import type { Profile } from '@/api/types';

vi.mock('@/api/nodes', () => ({ useNodeProfiles: vi.fn() }));

const profile = {
  id: 'p1',
  name: 'k3e6e1608d68c',
  key_label: 'Support team',
  access_key_id: 'a1',
  carrier_mode: 'https',
  limits: {},
  sync_state: 'synced',
} as unknown as Profile;

function mockProfiles(live: { data?: unknown; isError?: boolean; error?: unknown }) {
  vi.mocked(useNodeProfiles).mockImplementation(
    (_id: string, isLive?: boolean) =>
      (isLive
        ? { isLoading: false, isError: false, refetch: vi.fn(), ...live }
        : { data: { items: [profile], total: 1 }, isLoading: false, isError: false, refetch: vi.fn() }) as unknown as ReturnType<
        typeof useNodeProfiles
      >,
  );
}

describe('NodeProfilesTab', () => {
  beforeEach(() => setLang('en'));

  it('reports a failed comparison instead of listing every user as missing from the server', async () => {
    mockProfiles({ isError: true, error: new ApiError(500, 'internal', 'boom', {}) });
    render(<NodeProfilesTab nodeId="n1" online engine="telemt" />);

    await userEvent.click(screen.getByRole('button', { name: 'Compare with the server' }));

    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.queryByText('Only in the panel database')).toBeNull();
  });

  it('says the lists match when the server has the same users', async () => {
    mockProfiles({ data: { items: [{ name: 'k3e6e1608d68c', carrier_mode: 'https', limits: {} }], total: 1 } });
    render(<NodeProfilesTab nodeId="n1" online engine="telemt" />);

    await userEvent.click(screen.getByRole('button', { name: 'Compare with the server' }));

    expect(screen.getByText('Users match')).toBeInTheDocument();
  });
});
