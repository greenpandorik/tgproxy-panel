import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { DashboardMetrics } from './DashboardMetrics';

import type { DashboardMetricsProps } from './DashboardMetrics';

function renderMetrics(props: Partial<DashboardMetricsProps> = {}) {
  return render(
    <MemoryRouter>
      <DashboardMetrics nodesOnline={2} nodesTotal={3} keysActive={7} sessions={41} traffic={1_500_000} {...props} />
    </MemoryRouter>,
  );
}

describe('DashboardMetrics', () => {
  beforeEach(() => {
    setLang('en');
  });

  it('shows the four numbers that back the verdict', () => {
    renderMetrics();

    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('7')).toBeInTheDocument();
    expect(screen.getByText('41')).toBeInTheDocument();
    expect(screen.getByText('MB')).toBeInTheDocument();
  });

  it('keeps a real zero a zero', () => {
    renderMetrics({ sessions: 0 });

    expect(screen.getByText('0')).toHaveAttribute('data-metric', 'present');
  });

  it('says a figure is missing instead of showing it as zero', () => {
    renderMetrics({ sessions: undefined, traffic: undefined, keysActive: null });

    expect(screen.getAllByText('Not available')).toHaveLength(3);
    expect(screen.queryByText('0')).toBeNull();
    expect(screen.queryByText('MB')).toBeNull();
  });
});
