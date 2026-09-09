export type ThemePreference = 'light' | 'dark' | 'system';
export type Density = 'comfortable' | 'compact';

/** Storage can be unavailable in private or embedded browsers. Preferences still work in memory. */
export function readPreference(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}
export function savePreference(key: string, value: string | null) {
  try {
    if (value === null) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, value);
  } catch {
    /* Storage is optional. */
  }
}
export function readThemePreference(): ThemePreference | null {
  const value = readPreference('theme');
  return value === 'light' || value === 'dark' || value === 'system' ? value : null;
}
