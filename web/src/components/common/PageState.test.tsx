import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { Button } from '@/components/ui/button';

import { DegradedState, LoadingState, NotAvailableState } from './PageState';

describe('page states', () => {
  beforeEach(() => {
    setLang('en');
  });

  it('announces loading without pretending to have numbers', () => {
    render(<LoadingState rows={4} />);

    const region = screen.getByRole('status');
    expect(region).toHaveAttribute('aria-busy', 'true');
    expect(region).toHaveTextContent('Loading');
    expect(region.querySelectorAll('[data-slot="skeleton"]')).toHaveLength(4);
  });

  it('says a partial answer is partial', () => {
    render(<DegradedState />);

    expect(screen.getByText('Partial data')).toBeInTheDocument();
    expect(screen.getByRole('status')).toHaveTextContent('Some sources did not answer');
  });

  it('keeps not available apart from empty and carries its own actions', () => {
    render(<NotAvailableState description="WEB runtime did not respond." action={<Button>Retry</Button>} />);

    expect(screen.getByText('Not available')).toBeInTheDocument();
    expect(screen.getByText('WEB runtime did not respond.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });

  it('speaks Russian', () => {
    setLang('ru');
    render(<NotAvailableState />);

    expect(screen.getByText('Нет данных')).toBeInTheDocument();
  });
});
