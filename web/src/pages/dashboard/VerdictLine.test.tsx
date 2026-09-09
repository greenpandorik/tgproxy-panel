import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { VerdictLine } from './VerdictLine';

import type { FleetFacts } from './verdict';

const fleet = (facts: Partial<FleetFacts> = {}): FleetFacts => ({
  online: 3,
  total: 3,
  offline: 0,
  degraded: 0,
  attention: 0,
  ...facts,
});

describe('VerdictLine', () => {
  beforeEach(() => {
    setLang('en');
  });

  it('opens with the all-clear sentence and the count behind it', () => {
    const { container } = render(<VerdictLine facts={fleet()} />);

    expect(screen.getByText('All systems operational')).toBeInTheDocument();
    expect(container.querySelector('[data-verdict]')).toHaveAttribute('data-verdict', 'healthy');
    expect(screen.getByText('Online:')).toBeInTheDocument();
    expect(screen.queryByText('Issues:')).toBeNull();
  });

  it('says what is wrong, and how much of it, when problems are open', () => {
    const { container } = render(<VerdictLine facts={fleet({ online: 2, offline: 1, attention: 1 })} />);

    expect(screen.getByText('Some servers are not responding')).toBeInTheDocument();
    expect(container.querySelector('[data-verdict]')).toHaveAttribute('data-verdict', 'degraded');
    expect(screen.getByText('Issues:')).toBeInTheDocument();
  });

  it('never passes off a missing count as zero', () => {
    const { container } = render(<VerdictLine facts={fleet({ online: undefined, total: undefined })} />);

    expect(screen.getByText('The panel could not read the state of the fleet')).toBeInTheDocument();
    expect(container.querySelector('[data-verdict]')).toHaveAttribute('data-verdict', 'unknown');
    expect(screen.getAllByText('Not available')).toHaveLength(2);
    expect(screen.queryByText('0')).toBeNull();
  });

  it('speaks Russian too', () => {
    setLang('ru');
    render(<VerdictLine facts={fleet()} />);

    expect(screen.getByText('Всё работает')).toBeInTheDocument();
    expect(screen.getByText('На связи:')).toBeInTheDocument();
  });
});
