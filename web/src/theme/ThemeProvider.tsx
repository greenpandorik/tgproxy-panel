import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { useBranding } from '@/api/branding';
import { DEFAULT_PANEL_NAME } from '@/components/brand/brand';
import { brandForeground } from './contrast';
import { DEFAULT_PRIMARY_COLOR } from '@/components/brand/brand';
import { readPreference, readThemePreference, savePreference } from './preferences';
import type { Density, ThemePreference } from './preferences';
import type { Branding } from '@/api/types';

export type Theme = 'dark' | 'light';
export type BrandingPreview = Partial<Branding>;
interface ThemeContextValue {
  theme: Theme;
  preference: ThemePreference;
  setTheme: (t: ThemePreference) => void;
  toggleTheme: () => void;
  density: Density;
  setDensity: (d: Density) => void;
  collapsed: boolean;
  setCollapsed: (c: boolean) => void;
  resetPreferences: () => void;
  branding: BrandingPreview | undefined;
  previewBranding: (override: BrandingPreview | null) => void;
}
const ThemeContext = createContext<ThemeContextValue | null>(null);
export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error('useTheme must be used within ThemeProvider');
  return ctx;
}
/** Optional context lets public identity components also render in isolation. */
export function useBrandingIdentity() {
  const { data } = useBranding();
  const ctx = useContext(ThemeContext);
  return { branding: ctx?.branding ?? data, theme: ctx?.theme ?? 'light' };
}
export function ThemeProvider({ children }: { children: ReactNode }) {
  const { data: savedBranding } = useBranding();
  const [choice, setChoice] = useState<ThemePreference | null>(readThemePreference);
  const [systemDark, setSystemDark] = useState(() => window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false);
  const [density, setDensityState] = useState<Density>(() =>
    readPreference('panel-density') === 'compact' ? 'compact' : 'comfortable',
  );
  const [collapsed, setCollapsedState] = useState(() => readPreference('sidebar-collapsed') === '1');
  const [preview, setPreview] = useState<BrandingPreview | null>(null);
  const branding = useMemo(
    () => (savedBranding || preview ? { ...savedBranding, ...preview } : undefined),
    [savedBranding, preview],
  );
  const preference = choice ?? branding?.theme_default ?? 'light';
  const theme: Theme = preference === 'system' ? (systemDark ? 'dark' : 'light') : preference;

  useEffect(() => {
    const media = window.matchMedia?.('(prefers-color-scheme: dark)');
    if (!media) return;
    const update = () => setSystemDark(media.matches);
    media.addEventListener('change', update);
    return () => media.removeEventListener('change', update);
  }, []);
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);
  useEffect(() => {
    document.documentElement.dataset.density = density;
  }, [density]);
  useEffect(() => {
    document.documentElement.style.setProperty(
      '--primary-foreground',
      brandForeground(branding?.primary_color || DEFAULT_PRIMARY_COLOR),
    );
    for (const [name, value] of [
      ['--brand-primary', branding?.primary_color],
      ['--brand-accent', branding?.accent_color],
    ]) {
      if (value) document.documentElement.style.setProperty(name!, value);
      else document.documentElement.style.removeProperty(name!);
    }
  }, [branding?.primary_color, branding?.accent_color]);
  useEffect(() => {
    document.title = branding?.panel_name || DEFAULT_PANEL_NAME;
  }, [branding?.panel_name]);
  useEffect(() => {
    let link = document.querySelector<HTMLLinkElement>('#favicon');
    if (!link) {
      link = document.createElement('link');
      link.id = 'favicon';
      link.rel = 'icon';
      document.head.appendChild(link);
    }
    link.href = branding?.favicon_url || '/favicon.svg';
    link.removeAttribute('type');
  }, [branding?.favicon_url]);
  useEffect(() => {
    let style = document.getElementById('brand-css');
    if (!branding?.custom_css) {
      style?.remove();
      return;
    }
    if (!style) {
      style = document.createElement('style');
      style.id = 'brand-css';
      document.head.appendChild(style);
    }
    style.textContent = branding.custom_css;
  }, [branding?.custom_css]);
  const value = useMemo<ThemeContextValue>(() => {
    const setTheme = (next: ThemePreference) => {
      savePreference('theme', next);
      setChoice(next);
    };
    return {
      theme,
      preference,
      setTheme,
      toggleTheme: () => setTheme(theme === 'dark' ? 'light' : 'dark'),
      density,
      setDensity: (next) => {
        savePreference('panel-density', next);
        setDensityState(next);
      },
      collapsed,
      setCollapsed: (next) => {
        savePreference('sidebar-collapsed', next ? '1' : '0');
        setCollapsedState(next);
      },
      resetPreferences: () => {
        ['theme', 'panel-density', 'sidebar-collapsed'].forEach((key) => savePreference(key, null));
        setChoice(null);
        setDensityState('comfortable');
        setCollapsedState(false);
      },
      branding,
      previewBranding: setPreview,
    };
  }, [theme, preference, density, collapsed, branding]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}
