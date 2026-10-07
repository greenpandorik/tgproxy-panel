import { render, screen } from '@testing-library/react';
import { useState } from 'react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { EMPTY_TELEMT_LIMITS_FORM, telemtLimitsFromForm } from '@/lib/units';

import { TelemtLimitsFields } from './TelemtLimitsFields';

import type { TelemtLimitsForm } from '@/lib/units';

function Harness({ onValue }: { onValue: (v: TelemtLimitsForm) => void }) {
  const [value, setValue] = useState<TelemtLimitsForm>(EMPTY_TELEMT_LIMITS_FORM);
  return (
    <TelemtLimitsFields
      value={value}
      onChange={(next) => {
        setValue(next);
        onValue(next);
      }}
    />
  );
}

describe('TelemtLimitsFields quota period', () => {
  beforeEach(() => {
    void setLang('en');
  });

  it('asks for a quota first, then sends the chosen period', async () => {
    const user = userEvent.setup();
    let last = EMPTY_TELEMT_LIMITS_FORM;
    render(<Harness onValue={(v) => (last = v)} />);

    const period = screen.getByRole('combobox', { name: 'Quota resets' });
    expect(period).toHaveAttribute('data-disabled');
    expect(screen.getByText('Set a traffic quota to reset it on a schedule.')).toBeInTheDocument();

    await user.type(screen.getByLabelText('Traffic quota, GB'), '50');
    expect(period).not.toHaveAttribute('data-disabled');
    expect(screen.getByText('The quota counts all traffic and never resets on its own.')).toBeInTheDocument();

    await user.click(period);
    await user.click(await screen.findByRole('option', { name: 'Every month' }));
    expect(await screen.findByText(/^Next reset on .+, 00:00 UTC\.$/)).toBeInTheDocument();
    expect(telemtLimitsFromForm(last).data_quota_period).toBe('month');
  });
});
