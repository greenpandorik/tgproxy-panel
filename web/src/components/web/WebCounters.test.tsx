import { render, screen } from '@testing-library/react';
import { beforeEach, expect, it } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { WebCounters } from './WebCounters';

import type { WebCarrierStats } from '@/api/types';

const stats = (over: Partial<WebCarrierStats> = {}): WebCarrierStats => ({
  from: '2026-09-27T10:00:00Z',
  to: '2026-09-28T10:00:00Z',
  samples: 1440,
  counter_resets: 0,
  carrier_selections: 812,
  carrier_failures: 0,
  rejected_attempts: 0,
  evicted_sessions: 0,
  bridge_recoveries: 0,
  learning_entries: 14,
  carrier_selection_distribution: [],
  ...over,
});

beforeEach(() => setLang('en'));

it('says nothing went wrong in one line and keeps the counters folded away', () => {
  render(<WebCounters stats={stats()} />);
  expect(screen.getByTestId('web-counters-summary')).toHaveTextContent('No carrier failures and no rejected or evicted sessions');
  expect(screen.getByTestId('web-counter-carrier_failures')).not.toBeVisible();
});

it('shows every counter as soon as one of them is not zero', () => {
  render(<WebCounters stats={stats({ rejected_attempts: 3 })} />);
  expect(screen.queryByTestId('web-counters-summary')).toBeNull();
  expect(screen.getByTestId('web-counter-rejected_attempts')).toBeVisible();
});

it('does not claim a quiet window when a counter was not reported', () => {
  render(<WebCounters stats={stats({ evicted_sessions: null })} />);
  expect(screen.getByTestId('web-counters-summary')).toHaveTextContent('does not send some of these counters');
});

it('does not speak of failures when the server reported no counter at all', () => {
  render(
    <WebCounters
      stats={stats({ carrier_failures: null, rejected_attempts: null, evicted_sessions: null, bridge_recoveries: null })}
    />,
  );
  expect(screen.getByTestId('web-counters-summary')).toHaveTextContent('The server sent no failure counters in this window.');
});
