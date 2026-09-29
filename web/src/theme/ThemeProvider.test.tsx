import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import { ThemeProvider, useTheme } from './ThemeProvider';

function Controls() {
  const theme = useTheme();
  return (
    <>
      <span data-testid="theme">{theme.theme}</span>
      <button onClick={() => theme.setTheme('system')}>System</button>
      <button onClick={theme.toggleTheme}>Toggle</button>
      <span data-testid="primary">{theme.branding?.primary_color}</span>
      <button onClick={() => theme.setDensity('compact')}>Compact</button>
      <button onClick={() => theme.setCollapsed(true)}>Collapse</button>
      <button onClick={theme.resetPreferences}>Reset</button>
      <button onClick={() => theme.previewBranding({ panel_name: 'Preview', favicon_url: '/preview.ico' })}>Preview</button>
      <button onClick={() => theme.previewBranding(null)}>Cancel</button>
    </>
  );
}
function mount() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <ThemeProvider>
        <Controls />
      </ThemeProvider>
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal(
    'fetch',
    vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ panel_name: 'Saved project', theme_default: 'light', favicon_url: '' }), {
          headers: { 'content-type': 'application/json' },
        }),
      ),
    ),
  );
});
it('maps every earlier default pair to the theme pair and updates chart identity when toggled', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(
      JSON.stringify({ primary_color: '#e23c92', accent_color: '#12a198', theme_default: 'light', panel_name: 'Legacy' }),
      { headers: { 'content-type': 'application/json' } },
    ),
  );
  mount();
  await waitFor(() => expect(document.title).toBe('Legacy'));
  expect(screen.getByTestId('primary')).toHaveTextContent('#0b7285');
  expect(document.documentElement.style.getPropertyValue('--primary-foreground')).toBe('#ffffff');
  await userEvent.click(screen.getByText('Toggle'));
  expect(screen.getByTestId('primary')).toHaveTextContent('#3fc0d6');
  expect(document.documentElement.style.getPropertyValue('--brand-accent')).toBe('#20c997');
  expect(document.documentElement.style.getPropertyValue('--primary-foreground')).toBe('#000000');
});
it('keeps custom brand colors in both themes', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(
      JSON.stringify({ primary_color: '#112233', accent_color: '#abcdef', theme_default: 'light', panel_name: 'Custom' }),
      { headers: { 'content-type': 'application/json' } },
    ),
  );
  mount();
  await waitFor(() => expect(document.title).toBe('Custom'));
  await userEvent.click(screen.getByText('Toggle'));
  expect(document.documentElement.style.getPropertyValue('--brand-primary')).toBe('#112233');
  expect(document.documentElement.style.getPropertyValue('--brand-accent')).toBe('#abcdef');
  expect(screen.getByTestId('primary')).toHaveTextContent('#112233');
});
it('persists density and menu, then resets them without changing the saved brand', async () => {
  const user = userEvent.setup();
  const first = mount();
  await waitFor(() => expect(document.title).toBe('Saved project'));
  await user.click(screen.getByText('Compact'));
  await user.click(screen.getByText('Collapse'));
  expect(document.documentElement.dataset.density).toBe('compact');
  expect(localStorage.getItem('sidebar-collapsed')).toBe('1');
  first.unmount();
  mount();
  expect(document.documentElement.dataset.density).toBe('compact');
  await user.click(screen.getByText('Reset'));
  expect(document.documentElement.dataset.density).toBe('comfortable');
  expect(localStorage.getItem('sidebar-collapsed')).toBeNull();
});
it('follows operating-system theme changes and keeps the preference after remount', async () => {
  let listener: (() => void) | undefined;
  const media = {
    matches: false,
    addEventListener: (_: string, fn: () => void) => {
      listener = fn;
    },
    removeEventListener: vi.fn(),
  };
  vi.stubGlobal('matchMedia', () => media);
  const user = userEvent.setup();
  const first = mount();
  await user.click(screen.getByText('System'));
  act(() => {
    media.matches = true;
    listener?.();
  });
  expect(screen.getByTestId('theme')).toHaveTextContent('dark');
  first.unmount();
  mount();
  expect(screen.getByTestId('theme')).toHaveTextContent('dark');
  expect(localStorage.getItem('theme')).toBe('system');
});
it('restores the saved title and default favicon when a preview ends', async () => {
  mount();
  const user = userEvent.setup();
  await waitFor(() => expect(document.title).toBe('Saved project'));
  await user.click(screen.getByText('Preview'));
  expect(document.title).toBe('Preview');
  expect(document.querySelector('#favicon')).toHaveAttribute('href', '/preview.ico');
  await user.click(screen.getByText('Cancel'));
  expect(document.title).toBe('Saved project');
  expect(document.querySelector('#favicon')).toHaveAttribute('href', '/favicon.svg');
});
