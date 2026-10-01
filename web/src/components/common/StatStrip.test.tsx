import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';

import { StatStrip } from './StatStrip';

describe('StatStrip', () => {
  it('shows each number with its label and links the ones that lead somewhere', () => {
    render(
      <MemoryRouter>
        <StatStrip
          label="Сводка"
          items={[
            { id: 'people', label: 'Люди онлайн', value: '≈ 8', sub: 'соединений: 87', to: '/monitoring' },
            { id: 'nodes', label: 'Серверы на связи', value: '2 / 3', tone: 'warn' },
          ]}
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole('group', { name: 'Сводка' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Люди онлайн/ })).toHaveAttribute('href', '/monitoring');
    expect(screen.getByText('соединений: 87')).toBeInTheDocument();
    expect(screen.getByText('2 / 3')).toHaveClass('text-warn');
  });

  it('hides the numbers while loading', () => {
    render(
      <MemoryRouter>
        <StatStrip loading items={[{ id: 'people', label: 'Люди онлайн', value: '≈ 8', sub: 'соединений: 87' }]} />
      </MemoryRouter>,
    );
    expect(screen.queryByText('≈ 8')).not.toBeInTheDocument();
    expect(screen.queryByText('соединений: 87')).not.toBeInTheDocument();
  });
});
