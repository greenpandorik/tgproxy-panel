import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { usePublicStatus } from '@/api/status';
import { useUpdateStatus } from '@/api/updates';

import { StatusChips, StatusMenuRows } from './StatusChips';

import type { PublicStatus } from '@/api/status';
import type { UpdateStatus } from '@/api/updates';

vi.mock('@/api/updates', () => ({ useUpdateStatus: vi.fn() }));
vi.mock('@/api/status', () => ({ usePublicStatus: vi.fn() }));

const UPDATE: UpdateStatus = {
  enabled: true,
  current: '1.2.0',
  latest: '1.3.0',
  latest_url: 'https://github.com/greenpandorik/tgproxy-panel/releases/tag/v1.3.0',
  published_at: '2026-09-01T00:00:00Z',
  stars: 6096,
  repo_url: 'https://github.com/greenpandorik/tgproxy-panel',
  update_available: true,
  checked_at: '2026-09-06T00:00:00Z',
  stale: false,
};

const STATUS: PublicStatus = { version: '1.2.0', nodes_total: 3, nodes_online: 2, relay_commit: 'abc' };

function mockHooks(update: Partial<UpdateStatus> | undefined, status: Partial<PublicStatus> | undefined = {}) {
  vi.mocked(useUpdateStatus).mockReturnValue({
    data: update === undefined ? undefined : { ...UPDATE, ...update },
  } as unknown as ReturnType<typeof useUpdateStatus>);
  vi.mocked(usePublicStatus).mockReturnValue({
    data: status === undefined ? undefined : { ...STATUS, ...status },
  } as unknown as ReturnType<typeof usePublicStatus>);
}

function renderChips() {
  return render(
    <MemoryRouter>
      <StatusChips />
    </MemoryRouter>,
  );
}

describe('StatusChips', () => {
  beforeEach(() => {
    setLang('ru');
  });

  it('renders the current version as a plain chip when no update is available', () => {
    mockHooks({ update_available: false, latest: '1.2.0' });
    renderChips();

    const chip = screen.getByTestId('version-chip');
    expect(chip).toHaveTextContent('v1.2.0');
    expect(chip.tagName).toBe('SPAN');
    expect(chip).not.toHaveAttribute('data-update', 'true');
  });

  it('shows "dev" without a v prefix', () => {
    mockHooks({ current: 'dev', update_available: false });
    renderChips();

    expect(screen.getByTestId('version-chip')).toHaveTextContent(/^dev$/);
  });

  it('links to the release and is highlighted when an update is available', () => {
    mockHooks({});
    renderChips();

    const chip = screen.getByTestId('version-chip');
    expect(chip.tagName).toBe('A');
    expect(chip).toHaveAttribute('href', UPDATE.latest_url);
    expect(chip).toHaveAttribute('target', '_blank');
    expect(chip).toHaveAttribute('rel', 'noreferrer');
    expect(chip).toHaveAttribute('data-update', 'true');
    expect(chip).toHaveAttribute('aria-label', 'Доступна версия v1.3.0');
  });

  it('is a plain chip, not a link, when the update check is disabled', () => {
    mockHooks({ enabled: false, update_available: true });
    renderChips();

    const chip = screen.getByTestId('version-chip');
    expect(chip.tagName).toBe('SPAN');
    expect(chip).toHaveTextContent('v1.2.0');
    expect(chip).not.toHaveAttribute('data-update', 'true');
  });

  it('renders the GitHub chip with the compact star count', () => {
    mockHooks({});
    renderChips();

    const chip = screen.getByTestId('github-chip');
    expect(chip).toHaveAttribute('href', UPDATE.repo_url);
    expect(chip).toHaveTextContent('6.1k');
  });

  it('renders the GitHub chip with the mark only when stars are unknown', () => {
    mockHooks({ stars: -1 });
    renderChips();

    const chip = screen.getByTestId('github-chip');
    expect(chip).toHaveAttribute('href', UPDATE.repo_url);
    expect(chip).toHaveTextContent(/^$/);
  });

  it('renders nodes online as online/total with a tone', () => {
    mockHooks({}, { nodes_total: 3, nodes_online: 2 });
    renderChips();

    const chip = screen.getByTestId('nodes-chip');
    expect(chip).toHaveTextContent('2/3');
    expect(chip).toHaveAttribute('data-tone', 'warn');
    expect(chip).toHaveAttribute('href', '/nodes');
  });

  it.each([
    [3, 3, 'ok'],
    [3, 0, 'err'],
    [0, 0, 'dim'],
  ])('total %i online %i -> tone %s', (total, online, tone) => {
    mockHooks({}, { nodes_total: total, nodes_online: online });
    renderChips();

    expect(screen.getByTestId('nodes-chip')).toHaveAttribute('data-tone', tone);
  });

  it('renders nothing for the version chip until the update status has loaded', () => {
    mockHooks(undefined, {});
    renderChips();

    expect(screen.queryByTestId('version-chip')).toBeNull();
    expect(screen.queryByTestId('github-chip')).toBeNull();
    expect(screen.getByTestId('nodes-chip')).toHaveTextContent('2/3');
  });
});

describe('StatusMenuRows', () => {
  beforeEach(() => {
    setLang('en');
  });

  it('lists version, stars and nodes for the mobile menu', () => {
    mockHooks({}, {});
    render(
      <MemoryRouter>
        <StatusMenuRows />
      </MemoryRouter>,
    );

    expect(screen.getByText('v1.2.0')).toBeInTheDocument();
    expect(screen.getByText('v1.3.0 is available')).toBeInTheDocument();
    expect(screen.getByText('6.1k')).toBeInTheDocument();
    expect(screen.getByText('2/3')).toBeInTheDocument();
  });
});
