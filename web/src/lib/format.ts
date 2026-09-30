
import i18next from 'i18next';

const BYTE_UNITS = {
  en: ['B', 'KB', 'MB', 'GB', 'TB', 'PB'],
  ru: ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ', 'ПБ'],
} as const;

/** Byte units in the UI language, smallest first. */
export function byteUnits(lang: string | undefined = i18next.language): readonly string[] {
  return lang?.startsWith('ru') ? BYTE_UNITS.ru : BYTE_UNITS.en;
}

/** Formats a byte count as a short "12.3 MB" style string (base 1024), with units in the UI language. */
export function formatBytes(bytes: number, digits = 1, lang?: string): string {
  const units = byteUnits(lang);
  if (!Number.isFinite(bytes) || bytes <= 0) return `0 ${units[0]}`;
  const exp = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** exp;
  return `${value.toFixed(exp === 0 ? 0 : digits)} ${units[exp]}`;
}

/** Formats an absolute date/time using the given locale (defaults to browser locale). */
export function formatDateTime(value: string | number | Date, locale?: string): string {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '—';
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(d);
}

export function formatDate(value: string | number | Date, locale?: string): string {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '—';
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(d);
}

/** Formats a timestamp as "3 минуты назад" / "3 minutes ago" style relative time. */
export function formatRelativeTime(value: string | number | Date, locale?: string, now = Date.now()): string {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '—';
  const diffSeconds = Math.round((d.getTime() - now) / 1000);
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });

  const table: [number, Intl.RelativeTimeFormatUnit][] = [
    [60, 'second'],
    [60, 'minute'],
    [24, 'hour'],
    [7, 'day'],
    [4.34524, 'week'],
    [12, 'month'],
    [Number.POSITIVE_INFINITY, 'year'],
  ];

  let duration = diffSeconds;
  for (const [amount, unit] of table) {
    if (Math.abs(duration) < amount) {
      return rtf.format(Math.round(duration), unit);
    }
    duration /= amount;
  }
  return rtf.format(Math.round(duration), 'year');
}

// Formats a span of seconds the way an operator reads it in a dense table: one number and a narrow unit.
export function formatCompactDuration(seconds: number, locale?: string): string {
  const total = Math.max(0, Math.round(seconds));
  const [value, unit] =
    total < 60
      ? [total, 'second']
      : total < 3600
        ? [Math.round(total / 60), 'minute']
        : total < 86400
          ? [Math.round(total / 3600), 'hour']
          : [Math.round(total / 86400), 'day'];
  return new Intl.NumberFormat(locale, { style: 'unit', unit, unitDisplay: 'narrow' }).format(value);
}

// How long ago a timestamp was, as a compact duration ("3 ч").
export function formatCompactAge(value: string | number | Date | null | undefined, locale?: string, now = Date.now()): string | null {
  if (value === null || value === undefined || value === '') return null;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return null;
  return formatCompactDuration((now - d.getTime()) / 1000, locale);
}

/** Splits a byte count into the number and its unit, for a stat tile that sets them at different sizes. */
export function splitBytes(bytes: number, digits = 1, lang?: string): { value: string; unit: string } {
  const [value, unit = byteUnits(lang)[0]] = formatBytes(bytes, digits, lang).split(' ');
  return { value, unit };
}

/** Formats a plain integer with locale-aware thousands separators. */
export function formatNumber(value: number, locale?: string): string {
  return new Intl.NumberFormat(locale).format(value);
}

export function formatStars(stars: number): string {
  if (!Number.isFinite(stars) || stars < 0) return '';
  if (stars < 1000) return String(stars);
  const k = (stars / 1000).toFixed(1).replace(/\.0$/, '');
  return `${k}k`;
}
