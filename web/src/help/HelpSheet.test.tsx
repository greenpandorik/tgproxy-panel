import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { HelpButton } from './HelpButton';
import { HelpProvider } from './HelpProvider';
import { HelpSheet } from './HelpSheet';

describe('HelpSheet', () => {
  beforeEach(() => setLang('ru'));

  it('renders a topic: title, intro, fields with example and tip, notes, docs link', async () => {
    render(<HelpSheet topic="nodes.create" open onOpenChange={vi.fn()} />);

    expect(await screen.findByText('Новая нода')).toBeInTheDocument();
    expect(screen.getByText(/A-запись домена, которая уже указывает/)).toBeInTheDocument();
    // A field, its example (mono) and its tip.
    expect(screen.getByText('Домен Fake-TLS')).toBeInTheDocument();
    expect(screen.getAllByText('ams1.example.com').length).toBeGreaterThan(0);
    expect(screen.getByText(/Чужой домен подходит только если это реальный сайт/)).toBeInTheDocument();
    // A note.
    expect(screen.getByText(/Панель и нода — разные серверы/)).toBeInTheDocument();
    // The guide link follows the language.
    expect(screen.getByRole('link', { name: /руководстве/ })).toHaveAttribute(
      'href',
      'https://github.com/greenpandorik/tgproxy-panel/blob/main/docs/setup.ru.md#6-подключение-ноды',
    );
  });

  it('switches to English with the rest of the UI', async () => {
    setLang('en');
    render(<HelpSheet topic="keys.limits" open onOpenChange={vi.fn()} />);

    expect(await screen.findByText('Key limits')).toBeInTheDocument();
    expect(screen.getByText('Traffic quota, GB')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /setup guide/ })).toHaveAttribute(
      'href',
      'https://github.com/greenpandorik/tgproxy-panel/blob/main/docs/setup.en.md#7-node-engine-telemt-or-tproxy',
    );
  });
});

describe('HelpButton', () => {
  beforeEach(() => setLang('ru'));

  it('opens the sheet for its topic without a provider', async () => {
    const user = userEvent.setup();
    render(<HelpButton topic="audit" />);

    expect(screen.queryByText('Журнал аудита')).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Справка' }));
    expect(await screen.findByText('Журнал аудита')).toBeInTheDocument();
  });
});

describe('HelpProvider', () => {
  beforeEach(() => setLang('ru'));

  function renderAt(path: string) {
    return render(
      <MemoryRouter initialEntries={[path]}>
        <HelpProvider>
          <input aria-label="field" />
          <button type="button">noop</button>
        </HelpProvider>
      </MemoryRouter>,
    );
  }

  it('opens the help for the current route on `?` when no text field is focused', async () => {
    renderAt('/keys');
    screen.getByRole('button', { name: 'noop' }).focus();

    fireEvent.keyDown(window, { key: '?' });

    expect(await screen.findByText(/Ключ — это доступ к прокси/)).toBeInTheDocument();
  });

  it('leaves `?` alone while typing in a field', async () => {
    renderAt('/keys');
    screen.getByRole('textbox', { name: 'field' }).focus();

    fireEvent.keyDown(window, { key: '?' });

    await waitFor(() => expect(screen.queryByText(/Ключ — это доступ к прокси/)).not.toBeInTheDocument());
  });
});
