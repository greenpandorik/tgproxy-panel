import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';

import { useBranding } from '@/api/branding';

import type { Branding } from '@/api/types';

export type Theme = 'dark' | 'light';

const THEME_STORAGE_KEY = 'theme';
const BRAND_CSS_ID = 'brand-css';
const DEFAULT_TITLE = 'WEB Proxy Panel';

/** Subset of Branding the settings form can preview live, before saving. */
export type BrandingPreview = Partial<
  Pick<Branding, 'panel_name' | 'favicon_url' | 'primary_color' | 'accent_color' | 'theme_default' | 'custom_css'>
>;

interface ThemeContextValue {
  theme: Theme;
  setTheme: (t: Theme) => void;
  toggleTheme: () => void;
  /** Applies branding field overrides to the live page (colors, title, favicon, custom CSS, theme) until cleared with `null`. Used by the Branding settings form for a live preview before Save. */
  previewBranding: (override: BrandingPreview | null) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error('useTheme must be used within ThemeProvider');
  return ctx;
}

function storedTheme(): Theme | null {
  const v = window.localStorage.getItem(THEME_STORAGE_KEY);
  return v === 'dark' || v === 'light' ? v : null;
}

function applyFavicon(url: string | undefined): void {
  if (!url) return;
  let link = document.querySelector<HTMLLinkElement>('#favicon');
  if (!link) {
    link = document.createElement('link');
    link.id = 'favicon';
    link.rel = 'icon';
    document.head.appendChild(link);
  }
  link.href = url;
}

function applyCustomCss(css: string | undefined): void {
  let style = document.getElementById(BRAND_CSS_ID) as HTMLStyleElement | null;
  if (!css) {
    style?.remove();
    return;
  }
  if (!style) {
    style = document.createElement('style');
    style.id = BRAND_CSS_ID;
    document.head.appendChild(style);
  }
  style.textContent = css;
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const { data: branding } = useBranding();
  const [theme, setThemeState] = useState<Theme>(() => storedTheme() ?? 'dark');
  const [userChose, setUserChose] = useState<boolean>(() => storedTheme() !== null);
  // Tracks the last branding.theme_default we've reacted to, so the
  // "adopt the branding default until the user picks a theme" adjustment
  // below can run during render (React's documented pattern for state that
  // depends on a prop) instead of as a setState-in-effect cascade.
  const [appliedDefault, setAppliedDefault] = useState<Theme | undefined>(undefined);
  // Set by BrandingForm's live preview; null means "use the saved branding".
  const [preview, setPreview] = useState<BrandingPreview | null>(null);

  if (!userChose && branding?.theme_default && branding.theme_default !== appliedDefault) {
    setAppliedDefault(branding.theme_default);
    setThemeState(branding.theme_default);
  }

  // The branding form can preview a profile's default theme, but only for
  // someone who has not picked a theme themselves - exactly the audience that
  // default is for. Overriding an explicit choice made the settings page flip
  // to dark under an operator working in light, with the toggle apparently
  // doing nothing.
  useEffect(() => {
    document.documentElement.dataset.theme = !userChose && preview?.theme_default ? preview.theme_default : theme;
  }, [theme, userChose, preview?.theme_default]);

  useEffect(() => {
    const primary = preview?.primary_color ?? branding?.primary_color;
    const accent = preview?.accent_color ?? branding?.accent_color;
    if (primary) document.documentElement.style.setProperty('--brand-primary', primary);
    if (accent) document.documentElement.style.setProperty('--brand-accent', accent);
  }, [branding?.primary_color, branding?.accent_color, preview?.primary_color, preview?.accent_color]);

  useEffect(() => {
    document.title = preview?.panel_name ?? branding?.panel_name ?? DEFAULT_TITLE;
  }, [branding?.panel_name, preview?.panel_name]);

  useEffect(() => {
    applyFavicon(preview?.favicon_url ?? branding?.favicon_url);
  }, [branding?.favicon_url, preview?.favicon_url]);

  useEffect(() => {
    applyCustomCss(preview?.custom_css ?? branding?.custom_css);
  }, [branding?.custom_css, preview?.custom_css]);

  const value = useMemo<ThemeContextValue>(
    () => ({
      theme,
      setTheme: (t: Theme) => {
        window.localStorage.setItem(THEME_STORAGE_KEY, t);
        setUserChose(true);
        setThemeState(t);
      },
      toggleTheme: () => {
        setThemeState((prev) => {
          const next: Theme = prev === 'dark' ? 'light' : 'dark';
          window.localStorage.setItem(THEME_STORAGE_KEY, next);
          setUserChose(true);
          return next;
        });
      },
      previewBranding: setPreview,
    }),
    [theme],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}
