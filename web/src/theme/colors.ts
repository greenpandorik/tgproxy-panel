import type { Branding } from '@/api/types';

export const THEME_COLORS = {
  dark: { primary_color: '#3fc0d6', accent_color: '#20c997' },
  light: { primary_color: '#0b7285', accent_color: '#099268' },
} as const;

const DEFAULT_PAIRS = [
  ['#e23c92', '#12a198'],
  ['#3b82f6', '#22c55e'],
  ['#c4ed79', '#c0a8ed'],
  ['#365b46', '#a35336'],
  ['#0c8599', '#099268'],
  [THEME_COLORS.dark.primary_color, THEME_COLORS.dark.accent_color],
  [THEME_COLORS.light.primary_color, THEME_COLORS.light.accent_color],
];

/** True when the pair is one the panel shipped as its default, now or in an earlier release. */
export function isDefaultPair(primary: string | undefined, accent: string | undefined): boolean {
  const p = primary?.toLowerCase();
  const a = accent?.toLowerCase();
  return (!p && !a) || DEFAULT_PAIRS.some(([dp, da]) => dp === p && da === a);
}

/** Existing default profiles follow the selected theme; custom brand pairs stay intact. */
export function themeColors(branding: Partial<Branding> | undefined, theme: 'dark' | 'light') {
  if (isDefaultPair(branding?.primary_color, branding?.accent_color)) return THEME_COLORS[theme];
  return {
    primary_color: branding?.primary_color || THEME_COLORS[theme].primary_color,
    accent_color: branding?.accent_color || THEME_COLORS[theme].accent_color,
  };
}
