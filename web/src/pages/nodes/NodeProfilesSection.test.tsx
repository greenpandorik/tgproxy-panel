import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { useNodeProfiles } from '@/api/nodes';

import { NodeProfilesSection } from './NodeProfilesSection';

import type * as NodesApi from '@/api/nodes';

vi.mock('@/api/nodes', async (importOriginal) => ({
  ...(await importOriginal<typeof NodesApi>()),
  useNodeProfiles: vi.fn(),
}));

const dbRow = {
  id: 'p1',
  name: 'u_maria',
  access_key_id: 'k1',
  key_label: 'Мария',
  carrier_mode: 'tls',
  limits: {},
  sync_state: 'synced',
  key_expires_at: null,
};

function mockProfiles(db: unknown[], live: unknown[]) {
  vi.mocked(useNodeProfiles).mockImplementation(
    (_id: string, isLive = false) =>
      ({ isLoading: false, isError: false, data: { items: isLive ? live : db } }) as unknown as ReturnType<typeof useNodeProfiles>,
  );
}

describe('NodeProfilesSection', () => {
  beforeEach(() => {
    void setLang('ru');
  });

  it('shows how many users the server has and lists them when opened', async () => {
    mockProfiles([dbRow], [{ name: 'u_maria', carrier_mode: 'tls', limits: {} }]);
    render(<NodeProfilesSection nodeId="n1" online engine="telemt" />);
    expect(screen.getByText('Пользователи на сервере')).toBeInTheDocument();
    expect(screen.getByText('1')).toBeInTheDocument();
    await userEvent.click(screen.getByText('Пользователи на сервере'));
    expect(screen.getByText('u_maria')).toBeInTheDocument();
  });

  it('compares with the server and names what exists only on one side', async () => {
    mockProfiles([dbRow], [{ name: 'u_stray', carrier_mode: 'tls', limits: {} }]);
    render(<NodeProfilesSection nodeId="n1" online engine="telemt" />);
    await userEvent.click(screen.getByRole('button', { name: 'Сверить с сервером' }));
    expect(screen.getByText(/Только в базе панели/)).toBeInTheDocument();
    expect(screen.getByText(/Только на сервере/)).toBeInTheDocument();
    expect(screen.getByText('u_stray')).toBeInTheDocument();
  });

  it('says when the panel and the server agree', async () => {
    mockProfiles([dbRow], [{ name: 'u_maria', carrier_mode: 'tls', limits: {} }]);
    render(<NodeProfilesSection nodeId="n1" online engine="telemt" />);
    await userEvent.click(screen.getByRole('button', { name: 'Сверить с сервером' }));
    expect(screen.getByText('Пользователи совпадают')).toBeInTheDocument();
  });
});
