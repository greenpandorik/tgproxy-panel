import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { AuthProvider } from '@/auth/AuthProvider';
import { ThemeProvider } from '@/theme/ThemeProvider';

import { BrandingProfilesList } from './BrandingProfilesList';

import type { Branding, BrandingProfile } from '@/api/types';

const ME = { id: 'u1', username: 'root', role: 'owner', totp_enabled: false, features: { totp: true } };

const BASE: Omit<Branding, 'panel_name'> = {
  logo_url: '',
  favicon_url: '',
  primary_color: '#3b82f6',
  accent_color: '#22c55e',
  theme_default: 'dark',
  login_bg_url: '',
  login_text: '',
  support_link: '',
  footer_text: '',
  custom_css: '',
};

const ACTIVE_PROFILE: BrandingProfile = {
  ...BASE,
  id: 'p-active',
  name: 'Default',
  panel_name: 'Default Panel',
  is_active: true,
  updated_at: '2026-09-05T10:00:00Z',
};

const OTHER_PROFILE: BrandingProfile = {
  ...BASE,
  id: 'p-other',
  name: 'Winter',
  panel_name: 'Winter Panel',
  is_active: false,
  updated_at: '2026-09-01T10:00:00Z',
};

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function renderList() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AuthProvider>
          <ThemeProvider>
            <BrandingProfilesList />
          </ThemeProvider>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BrandingProfilesList', () => {
  beforeEach(() => {
    setLang('en');
    document.cookie = 'tgwp_csrf=test-csrf';
    globalThis.fetch = vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith('/auth/me')) return json(ME);
      if (path.endsWith('/branding/profiles')) return json({ items: [ACTIVE_PROFILE, OTHER_PROFILE] });
      if (path.endsWith('/branding')) return json({ panel_name: ACTIVE_PROFILE.panel_name, ...BASE });
      return json({ error: { code: 'not_found', message: 'nope' } }, 404);
    }) as typeof fetch;
  });

  it('shows the active badge on the active profile and hides Delete for it, while offering it for others', async () => {
    renderList();

    const list = within(await screen.findByRole('list', { name: 'Profiles' }));
    const activeRow = list.getByText('Default').closest('li');
    const otherRow = list.getByText('Winter').closest('li');
    if (!activeRow || !otherRow) throw new Error('profile rows not found');

    expect(within(activeRow).getByText('Active')).toBeInTheDocument();
    expect(within(otherRow).queryByText('Active')).toBeNull();

    expect(within(activeRow).queryByRole('button', { name: 'Delete' })).toBeNull();
    expect(within(otherRow).queryByRole('button', { name: 'Delete' })).not.toBeNull();
    expect(within(activeRow).queryByRole('button', { name: 'Activate' })).toBeNull();
    expect(within(otherRow).queryByRole('button', { name: 'Activate' })).not.toBeNull();
  });
});
