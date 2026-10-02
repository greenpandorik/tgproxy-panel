import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { NodeCapacityList } from './NodeCapacityList';

import type { Node } from '@/api/types';

const node = (i: number, name: string, hostname: string) =>
  ({ id: `n${i}`, name, hostname, profile_count: 3, max_profiles: 128 }) as unknown as Node;

const nodes = [
  node(1, 'Test2', 'speedy2.example.top'),
  node(2, 'WEB Speedy', 'web-speedy.example.com'),
  node(3, 'France 1', 'fr1.example.co'),
  node(4, 'Germany 1', 'de1.example.pro'),
  node(5, 'Finland', 'fi1.example.net'),
  node(6, 'Netherlands', 'nl1.example.net'),
];

describe('NodeCapacityList', () => {
  beforeEach(() => {
    void setLang('ru');
  });

  it('finds a server by name or address and keeps the selection count', async () => {
    const onChange = vi.fn();
    render(<NodeCapacityList nodes={nodes} selectedIds={['n1', 'n3']} onChange={onChange} />);
    expect(screen.getByText('выбрано 2 из 6')).toBeInTheDocument();

    await userEvent.type(screen.getByRole('searchbox', { name: 'Найти сервер' }), 'germ');
    expect(screen.getByText('Germany 1')).toBeInTheDocument();
    expect(screen.queryByText('France 1')).toBeNull();

    await userEvent.clear(screen.getByRole('searchbox'));
    await userEvent.type(screen.getByRole('searchbox'), 'nl1');
    expect(screen.getByText('Netherlands')).toBeInTheDocument();

    await userEvent.type(screen.getByRole('searchbox'), 'zzz');
    expect(screen.getByText('Ничего не найдено')).toBeInTheDocument();
  });

  it('has no search for a short list', () => {
    render(<NodeCapacityList nodes={nodes.slice(0, 3)} selectedIds={[]} onChange={vi.fn()} />);
    expect(screen.queryByRole('searchbox')).toBeNull();
  });
});
