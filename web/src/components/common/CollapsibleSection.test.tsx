import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { CollapsibleSection } from './CollapsibleSection';

describe('CollapsibleSection', () => {
  it('shows the summary while closed and opens from the keyboard', async () => {
    const user = userEvent.setup();
    render(
      <CollapsibleSection title="Службы" summary="telemt, caddy, агент работают" tone="ok">
        <p>Подробности</p>
      </CollapsibleSection>,
    );
    const trigger = screen.getByRole('button', { name: /Службы/ });
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByText('telemt, caddy, агент работают')).toBeInTheDocument();
    expect(screen.queryByText('Подробности')).toBeNull();

    trigger.focus();
    await user.keyboard('{Enter}');
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('Подробности')).toBeInTheDocument();
  });

  it('starts open when asked and keeps its action outside the toggle', async () => {
    const user = userEvent.setup();
    let ran = 0;
    render(
      <CollapsibleSection
        title="Проверки сервера"
        summary="2 не пройдены"
        tone="err"
        defaultOpen
        action={<button onClick={() => (ran += 1)}>Проверить сейчас</button>}
      >
        <p>Список проверок</p>
      </CollapsibleSection>,
    );
    const trigger = screen.getByRole('button', { name: /Проверки сервера/ });
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(trigger.closest('[data-slot="collapsible-section"]')).toHaveAttribute('data-tone', 'err');

    await user.click(screen.getByRole('button', { name: 'Проверить сейчас' }));
    expect(ran).toBe(1);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
  });
});
