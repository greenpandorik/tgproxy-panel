import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { MetricValue } from './MetricValue';
import { isMetricPresent } from './metric';

describe('MetricValue', () => {
  beforeEach(() => {
    setLang('en');
  });

  it('renders a real zero as zero', () => {
    render(<MetricValue value={0} />);

    const rendered = screen.getByText('0');
    expect(rendered).toHaveAttribute('data-metric', 'present');
    expect(screen.queryByText('Not available')).toBeNull();
  });

  it.each([[null], [undefined], [Number.NaN]])('renders %s as not available, not as zero', (value) => {
    render(<MetricValue value={value} />);

    expect(screen.getByText('Not available')).toHaveAttribute('data-metric', 'absent');
    expect(screen.queryByText('0')).toBeNull();
  });

  it('says it in Russian too', () => {
    setLang('ru');
    render(<MetricValue value={null} />);

    expect(screen.getByText('Нет данных')).toBeInTheDocument();
  });

  it('formats only a value that exists, and keeps the unit with it', () => {
    const format = (value: number) => value.toFixed(1);
    const { rerender } = render(<MetricValue value={0} format={format} unit="%" />);

    expect(screen.getByText('0.0')).toBeInTheDocument();
    expect(screen.getByText('%')).toBeInTheDocument();

    rerender(<MetricValue value={null} format={format} unit="%" />);
    expect(screen.getByText('Not available')).toBeInTheDocument();
    expect(screen.queryByText('%')).toBeNull();
  });

  it('shows a dash in dense rows while still saying it out loud', () => {
    render(<MetricValue value={undefined} compact />);

    const absent = screen.getByTitle('Not available');
    expect(absent).toHaveTextContent('—');
    expect(absent.querySelector('.sr-only')).toHaveTextContent('Not available');
  });

  it('tells an absent metric from a zero one', () => {
    expect(isMetricPresent(0)).toBe(true);
    expect(isMetricPresent('')).toBe(true);
    expect(isMetricPresent(null)).toBe(false);
    expect(isMetricPresent(undefined)).toBe(false);
    expect(isMetricPresent(Number.NaN)).toBe(false);
    expect(isMetricPresent(Number.POSITIVE_INFINITY)).toBe(false);
  });
});
