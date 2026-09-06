import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { StatCard } from './StatCard';

describe('StatCard', () => {
  it('renders label, value, unit, badge and context', () => {
    render(
      <StatCard
        label="Ноды онлайн"
        value={2}
        unit="/ 3"
        badge="1 офлайн"
        context="hel1 без heartbeat 3 ч"
      />,
    );

    expect(screen.getByText('Ноды онлайн')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getByText('/ 3')).toBeInTheDocument();
    expect(screen.getByText('1 офлайн')).toBeInTheDocument();
    expect(screen.getByText('hel1 без heartbeat 3 ч')).toBeInTheDocument();
  });

  it('colours the delta by tone', () => {
    const { rerender } = render(<StatCard label="Сессии" value={90} delta={{ text: '▲ 12%', tone: 'ok' }} />);
    expect(screen.getByText('▲ 12%')).toHaveClass('text-ok');

    rerender(<StatCard label="Сессии" value={90} delta={{ text: '▼ 40%', tone: 'err' }} />);
    expect(screen.getByText('▼ 40%')).toHaveClass('text-err');

    rerender(<StatCard label="Сессии" value={90} delta={{ text: '± 0%', tone: 'neutral' }} />);
    expect(screen.getByText('± 0%')).toHaveClass('text-dim');
  });

  it('hides the value and context behind skeletons while loading', () => {
    const { container } = render(<StatCard label="Ключи" value={7} context="4 ожидают" loading />);

    expect(screen.getByText('Ключи')).toBeInTheDocument();
    expect(screen.queryByText('7')).not.toBeInTheDocument();
    expect(screen.queryByText('4 ожидают')).not.toBeInTheDocument();
    expect(container.querySelectorAll('[data-slot="skeleton"]')).toHaveLength(2);
  });
});
