import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { HEALTH_STATUSES } from './healthStatus';
import { StatusIndicator } from './StatusIndicator';

describe('StatusIndicator', () => {
  beforeEach(() => {
    setLang('en');
  });

  it('covers the whole vocabulary with a word, an icon and a tone', () => {
    const seen = new Set<string>();

    for (const status of HEALTH_STATUSES) {
      const { unmount } = render(<StatusIndicator status={status} />);
      const badge = screen.getByText(new RegExp(`^${status}$`, 'i')).parentElement;

      expect(badge).toHaveAttribute('data-status', status);
      expect(badge?.querySelector('svg')).toBeInTheDocument();
      seen.add(badge?.getAttribute('data-tone') ?? '');
      unmount();
    }

    expect(HEALTH_STATUSES).toHaveLength(7);
    expect([...seen].sort()).toEqual(['err', 'info', 'neutral', 'ok', 'warn']);
  });

  it('never states the status by colour alone', () => {
    render(<StatusIndicator status="degraded" />);

    expect(screen.getByText('Degraded')).toBeVisible();
    expect(screen.getByText('Degraded').previousElementSibling?.querySelector('svg')).toBeInTheDocument();
  });

  it('keeps the wording for screen readers in the compact variant', () => {
    render(<StatusIndicator status="draining" hideLabel />);

    const text = screen.getByText('Draining');
    expect(text).toHaveClass('sr-only');
    expect(text.closest('[data-status]')?.querySelector('svg')).toBeInTheDocument();
  });

  it('speaks Russian and takes wording from the caller', () => {
    setLang('ru');
    const { rerender } = render(<StatusIndicator status="healthy" />);
    expect(screen.getByText('Работает')).toBeInTheDocument();

    rerender(<StatusIndicator status="healthy" label="Нода 1" />);
    expect(screen.getByText('Нода 1').closest('[data-status]')).toHaveAttribute('data-tone', 'ok');
  });
});
