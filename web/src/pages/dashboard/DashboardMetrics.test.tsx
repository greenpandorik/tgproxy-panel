import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { TooltipProvider } from '@/components/ui/tooltip';

import { DashboardMetrics } from './DashboardMetrics';

import type { DashboardMetricsProps } from './DashboardMetrics';

function renderMetrics(props: Partial<DashboardMetricsProps> = {}) {
  return render(
    <TooltipProvider delay={0}>
      <MemoryRouter>
        <DashboardMetrics
          nodesOnline={2}
          nodesTotal={3}
          keysActive={7}
          people={41}
          people15m={52}
          connections={180}
          traffic={1_500_000}
          {...props}
        />
      </MemoryRouter>
    </TooltipProvider>,
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
    expect(screen.getByText('MB')).toBeInTheDocument();
  });

  it('leads with people online and keeps the connections under them', () => {
    renderMetrics();

    expect(screen.getByText('People online')).toBeInTheDocument();
    expect(screen.getByText('≈ 41')).toBeInTheDocument();
    expect(screen.getByText('connections: 180')).toBeInTheDocument();
  });

  it('explains how people are counted', async () => {
    renderMetrics();

    await userEvent.hover(screen.getByText('People online'));
    expect(await screen.findByText(/behind one router count as one/)).toBeInTheDocument();
    expect(screen.getByText('In the last 15 minutes: ≈ 52')).toBeInTheDocument();
  });

  it('keeps a real zero a zero', () => {
    renderMetrics({ people: 0 });

    expect(screen.getByText('≈ 0')).toHaveAttribute('data-metric', 'present');
  });

  it('says a figure is missing instead of showing it as zero', () => {
    renderMetrics({ people: undefined, connections: undefined, traffic: undefined, keysActive: null });

    expect(screen.getAllByText('Not available')).toHaveLength(3);
    expect(screen.queryByText('≈ 0')).toBeNull();
    expect(screen.queryByText(/^connections:/)).toBeNull();
    expect(screen.queryByText('MB')).toBeNull();
  });
});
