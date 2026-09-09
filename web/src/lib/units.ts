
import type { TelemtLimits } from '@/api/types';

export const BYTES_PER_GB = 1024 ** 3;
export const BPS_PER_MBIT = 1_000_000;

/** Backend ceiling on a quota (internal/domain.MaxTelemtQuotaBytes = 100 TB), in GB. */
export const MAX_QUOTA_GB = 100 * 1024;

// Backend ceiling on the two counters (internal/domain.MaxTelemtCounter).
export const MAX_TELEMT_COUNTER = 1_000_000;

function tidy(value: number): number {
  return Math.round(value * 1e6) / 1e6;
}

/** Gigabytes (1024³ B) to bytes, rounded to a whole byte. */
export function gbToBytes(gb: number): number {
  if (!Number.isFinite(gb) || gb <= 0) return 0;
  return Math.round(gb * BYTES_PER_GB);
}

/** Bytes back to gigabytes for display in the form. */
export function bytesToGb(bytes: number): number {
  if (!Number.isFinite(bytes) || bytes <= 0) return 0;
  return tidy(bytes / BYTES_PER_GB);
}

/** Megabits per second to bits per second, rounded to a whole bit. */
export function mbitToBps(mbit: number): number {
  if (!Number.isFinite(mbit) || mbit <= 0) return 0;
  return Math.round(mbit * BPS_PER_MBIT);
}

/** Bits per second back to megabits per second for display in the form. */
export function bpsToMbit(bps: number): number {
  if (!Number.isFinite(bps) || bps <= 0) return 0;
  return tidy(bps / BPS_PER_MBIT);
}

export interface TelemtLimitsForm {
  quota_gb: string;
  rate_up_mbit: string;
  rate_down_mbit: string;
  max_unique_ips: string;
  max_tcp_conns: string;
}

export const EMPTY_TELEMT_LIMITS_FORM: TelemtLimitsForm = {
  quota_gb: '',
  rate_up_mbit: '',
  rate_down_mbit: '',
  max_unique_ips: '',
  max_tcp_conns: '',
};

/** A machine value as form text: 0 and absent both mean "no limit", which is an empty field. */
function text(value: number): string {
  return value > 0 ? String(value) : '';
}

export function telemtLimitsToForm(limits: TelemtLimits | undefined | null): TelemtLimitsForm {
  if (!limits) return { ...EMPTY_TELEMT_LIMITS_FORM };
  return {
    quota_gb: text(bytesToGb(limits.data_quota_bytes ?? 0)),
    rate_up_mbit: text(bpsToMbit(limits.rate_limit_up_bps ?? 0)),
    rate_down_mbit: text(bpsToMbit(limits.rate_limit_down_bps ?? 0)),
    max_unique_ips: text(limits.max_unique_ips ?? 0),
    max_tcp_conns: text(limits.max_tcp_conns ?? 0),
  };
}

// Reads a form field as a non-negative number.
export function parseAmount(raw: string): number {
  const trimmed = raw.trim().replace(',', '.');
  if (trimmed === '') return 0;
  const n = Number(trimmed);
  if (!Number.isFinite(n) || n <= 0) return 0;
  return n;
}

export function telemtLimitsFromForm(form: TelemtLimitsForm): TelemtLimits {
  return {
    data_quota_bytes: gbToBytes(parseAmount(form.quota_gb)),
    rate_limit_up_bps: mbitToBps(parseAmount(form.rate_up_mbit)),
    rate_limit_down_bps: mbitToBps(parseAmount(form.rate_down_mbit)),
    max_unique_ips: Math.trunc(parseAmount(form.max_unique_ips)),
    max_tcp_conns: Math.trunc(parseAmount(form.max_tcp_conns)),
  };
}

/** True when nothing in the form is set - the key runs unlimited. */
export function isEmptyTelemtLimits(limits: TelemtLimits | undefined | null): boolean {
  if (!limits) return true;
  return Object.values(limits).every((v) => !v);
}

// Which field of the form is out of range, as a stable message id the caller translates.
export function validateTelemtLimitsForm(form: TelemtLimitsForm): Partial<Record<keyof TelemtLimitsForm, string>> {
  const out: Partial<Record<keyof TelemtLimitsForm, string>> = {};
  for (const [name, raw] of Object.entries(form) as [keyof TelemtLimitsForm, string][]) {
    const trimmed = raw.trim().replace(',', '.');
    if (trimmed === '') continue;
    const n = Number(trimmed);
    if (!Number.isFinite(n) || n < 0) {
      out[name] = 'limit_positive';
      continue;
    }
    if (name === 'max_unique_ips' || name === 'max_tcp_conns') {
      if (!Number.isInteger(n)) {
        out[name] = 'limit_integer';
      } else if (n > MAX_TELEMT_COUNTER) {
        out[name] = 'limit_counter_max';
      }
    }
  }
  if (!out.quota_gb && parseAmount(form.quota_gb) > MAX_QUOTA_GB) {
    out.quota_gb = 'limit_quota_max';
  }
  return out;
}
