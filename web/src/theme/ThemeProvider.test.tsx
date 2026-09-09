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
