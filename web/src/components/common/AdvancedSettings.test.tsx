import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AdvancedSettings } from './AdvancedSettings';

describe('AdvancedSettings', () => {
  beforeEach(() => {
    setLang('en');
  });

  it('starts closed and announces that it is closed', () => {
    render(
      <AdvancedSettings>
        <p>Carrier policy</p>
      </AdvancedSettings>,
    );

    const toggle = screen.getByRole('button', { name: 'Show advanced settings' });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByText('Carrier policy')).not.toBeVisible();
  });

  it('opens from the keyboard and points at what it opened', async () => {
    const user = userEvent.setup();
    render(
      <AdvancedSettings>
        <p>Carrier policy</p>
      </AdvancedSettings>,
    );

    await user.tab();
    const toggle = screen.getByRole('button', { name: 'Show advanced settings' });
    expect(toggle).toHaveFocus();

    await user.keyboard('{Enter}');
    expect(screen.getByRole('button', { name: 'Hide advanced settings' })).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('Carrier policy')).toBeVisible();

    const controlled = toggle.getAttribute('aria-controls');
    expect(controlled).toBeTruthy();
    expect(document.getElementById(controlled as string)).toContainElement(screen.getByText('Carrier policy'));

    await user.keyboard('{Enter}');
    expect(screen.getByText('Carrier policy')).not.toBeVisible();
  });

  it('opens on demand and speaks Russian', () => {
    setLang('ru');
    render(
      <AdvancedSettings defaultOpen>
        <p>SNI</p>
      </AdvancedSettings>,
    );

    expect(screen.getByRole('button', { name: 'Скрыть расширенные настройки' })).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('SNI')).toBeVisible();
  });
});
