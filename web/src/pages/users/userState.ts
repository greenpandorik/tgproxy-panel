import type { StatTone } from '@/components/common/statTone';

const DAY = 86_400_000;

/** How close an expiry is: red once it has passed, amber inside a week. */
export function expiryTone(iso: string | null, now = Date.now()): StatTone {
  if (!iso) return 'neutral';
  const left = new Date(iso).getTime() - now;
  if (left <= 0) return 'err';
  if (left <= 7 * DAY) return 'warn';
  return 'ok';
}

export const TONE_TEXT: Record<StatTone, string> = {
  neutral: 'text-mute',
  ok: 'text-ok',
  warn: 'text-warn',
  err: 'text-err',
  info: 'text-info',
};

/** The short host label that tells servers apart at a glance. */
export function shortHost(hostname: string): string {
  return hostname.split('.')[0] || hostname;
}
