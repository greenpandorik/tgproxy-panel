/** From here a certificate is close to expiry; the panel's diagnostics warn at the same point. */
export const CERT_WARN_DAYS = 14;

/** A number of days in words: "61 день", "61 days". */
export function formatDays(days: number, locale?: string): string {
  return new Intl.NumberFormat(locale, { style: 'unit', unit: 'day', unitDisplay: 'long' }).format(Math.max(0, days));
}
