import type { Branding } from '@/api/types';

export const THEME_COLORS = {
  dark: { primary_color: '#3fc0d6', accent_color: '#20c997' },
  light: { primary_color: '#0c8599', accent_color: '#099268' },
} as const;

/** Existing default profiles follow the selected theme; custom brand pairs stay intact. */
export function themeColors(branding: Partial<Branding> | undefined, theme: 'dark' | 'light') {
  const primary = branding?.primary_color?.toLowerCase();
  const accent = branding?.accent_color?.toLowerCase();
  const defaults = [
    ['#e23c92', '#12a198'],
    ['#3b82f6', '#22c55e'],
    ['#c4ed79', '#c0a8ed'],
    ['#365b46', '#a35336'],
    [THEME_COLORS.dark.primary_color, THEME_COLORS.dark.accent_color],
    [THEME_COLORS.light.primary_color, THEME_COLORS.light.accent_color],
  ];
  if ((!primary && !accent) || defaults.some(([p, a]) => p === primary && a === accent)) return THEME_COLORS[theme];
  return {
    primary_color: branding?.primary_color || THEME_COLORS[theme].primary_color,
    accent_color: branding?.accent_color || THEME_COLORS[theme].accent_color,
  };
}
