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

  it('opens by itself while its summary is a problem, until it is closed by hand', async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <CollapsibleSection title="Датацентры Telegram" summary="все 5 доступны" tone="ok">
        <p>Таблица</p>
      </CollapsibleSection>,
    );
    const trigger = screen.getByRole('button', { name: /Датацентры Telegram/ });
    expect(trigger).toHaveAttribute('aria-expanded', 'false');

    rerender(
      <CollapsibleSection title="Датацентры Telegram" summary="2 недоступны" tone="warn">
        <p>Таблица</p>
      </CollapsibleSection>,
    );
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('2 недоступны').closest('.text-warn')).not.toBeNull();

    await user.click(trigger);
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
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
