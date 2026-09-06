import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { ThemeProvider } from '@/theme/ThemeProvider';

import { BrandingForm } from './BrandingForm';

import type { Branding, BrandingProfile } from '@/api/types';

const BASE: Branding = {
  panel_name: 'Base Panel',
  logo_url: '/logo-a.png',
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

const PROFILE_A: BrandingProfile = {
  ...BASE,
  id: 'p1',
  name: 'Default',
  is_active: true,
  updated_at: '2026-09-01T00:00:00Z',
};

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
}

function renderForm(profile: BrandingProfile) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const tree = (p: BrandingProfile) => (
    <QueryClientProvider client={qc}>
      <ThemeProvider>
        <BrandingForm profile={p} />
      </ThemeProvider>
    </QueryClientProvider>
  );
  const utils = render(tree(profile));
  return { ...utils, rerenderWith: (next: BrandingProfile) => utils.rerender(tree(next)) };
}

describe('BrandingForm', () => {
  beforeEach(() => {
    setLang('en');
    document.cookie = 'tgwp_csrf=test-csrf';
    // ThemeProvider fetches the public active branding on mount.
    globalThis.fetch = vi.fn(() => json(BASE)) as typeof fetch;
  });

  it('keeps unsaved edits to other fields when the same profile refetches with a new asset URL', async () => {
    const user = userEvent.setup();
    const { rerenderWith } = renderForm(PROFILE_A);

    const panelNameInput = await screen.findByLabelText('Panel name');
    await user.clear(panelNameInput);
    await user.type(panelNameInput, 'My typed name');
    expect(panelNameInput).toHaveValue('My typed name');

    // Same profile id, new object reference, only the logo URL changed - this simulates the
    // profiles query refetching after an unrelated mutation (e.g. an asset upload elsewhere
    // on the page invalidating the shared query) while the panel name field is still dirty.
    rerenderWith({ ...PROFILE_A, logo_url: '/logo-b.png' });

    expect(panelNameInput).toHaveValue('My typed name');
    expect(screen.getByAltText('Logo')).toHaveAttribute('src', '/logo-b.png');
  });

  it('does reset the form when a different profile is passed in (defensive - the id changed)', async () => {
    const user = userEvent.setup();
    const { rerenderWith } = renderForm(PROFILE_A);

    const panelNameInput = await screen.findByLabelText('Panel name');
    await user.clear(panelNameInput);
    await user.type(panelNameInput, 'Unsaved edit');
    expect(panelNameInput).toHaveValue('Unsaved edit');

    const otherProfile: BrandingProfile = { ...BASE, id: 'p2', name: 'Other', panel_name: 'Other Panel', is_active: false, updated_at: PROFILE_A.updated_at };
    rerenderWith(otherProfile);

    expect(await screen.findByLabelText('Panel name')).toHaveValue('Other Panel');
  });
});
