// Formatting helpers shared by tables and stat cards. Locale-aware bits take
// the active i18n language so numbers/dates read naturally in ru and en.

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'] as const;

/** Formats a byte count as a short "12.3 MB" style string (base 1024). */
export function formatBytes(bytes: number, digits = 1): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '0 B';
  if (bytes === 0) return '0 B';
  const exp = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), BYTE_UNITS.length - 1);
  const value = bytes / 1024 ** exp;
  return `${value.toFixed(exp === 0 ? 0 : digits)} ${BYTE_UNITS[exp]}`;
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

/**
 * Formats a span of seconds the way an operator reads it in a dense table:
 * one number and a narrow unit - "18 с", "5 мин", "3 ч", "2 дн." - instead of
 * a full sentence. Intl supplies the unit, so the abbreviations stay correct
 * per language without a translation key each.
 */
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

/**
 * How long ago a timestamp was, as a compact duration ("3 ч"). Callers wrap
 * it in `common.ago` to get "3 ч назад"; null means the timestamp is missing
 * or unparseable and the caller should show an em dash.
 */
export function formatCompactAge(value: string | number | Date | null | undefined, locale?: string, now = Date.now()): string | null {
  if (value === null || value === undefined || value === '') return null;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return null;
  return formatCompactDuration((now - d.getTime()) / 1000, locale);
}

/** Splits a byte count into the number and its unit, for a stat tile that sets them at different sizes. */
export function splitBytes(bytes: number, digits = 1): { value: string; unit: string } {
  const [value, unit = 'B'] = formatBytes(bytes, digits).split(' ');
  return { value, unit };
}

/** Formats a plain integer with locale-aware thousands separators. */
export function formatNumber(value: number, locale?: string): string {
  return new Intl.NumberFormat(locale).format(value);
}

/**
 * GitHub star count for a 7px-high chip: exact below a thousand, one decimal
 * with a `k` above it (6096 -> "6.1k"), trailing ".0" dropped (12000 -> "12k").
 * -1 is the backend's "unknown" and prints nothing, so the chip shows the mark alone.
 */
export function formatStars(stars: number): string {
  if (!Number.isFinite(stars) || stars < 0) return '';
  if (stars < 1000) return String(stars);
  const k = (stars / 1000).toFixed(1).replace(/\.0$/, '');
  return `${k}k`;
}
