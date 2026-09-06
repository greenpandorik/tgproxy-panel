import { describe, expect, it } from 'vitest';

import {
  BPS_PER_MBIT,
  BYTES_PER_GB,
  MAX_QUOTA_GB,
  MAX_TELEMT_COUNTER,
  bpsToMbit,
  bytesToGb,
  gbToBytes,
  isEmptyTelemtLimits,
  mbitToBps,
  parseAmount,
  telemtLimitsFromForm,
  telemtLimitsToForm,
  validateTelemtLimitsForm,
  EMPTY_TELEMT_LIMITS_FORM,
} from './units';

describe('gigabytes and bytes', () => {
  it('uses the same 1024 base the panel prints bytes in', () => {
    expect(gbToBytes(1)).toBe(BYTES_PER_GB);
    expect(gbToBytes(50)).toBe(50 * 1024 ** 3);
  });

  it('treats zero, negatives and nonsense as no limit', () => {
    expect(gbToBytes(0)).toBe(0);
    expect(gbToBytes(-4)).toBe(0);
    expect(gbToBytes(Number.NaN)).toBe(0);
    expect(bytesToGb(0)).toBe(0);
    expect(bytesToGb(-1)).toBe(0);
  });

  it('round-trips the fractional quotas an operator actually types', () => {
    for (const gb of [0.5, 1, 1.3, 2.75, 10, 100, 512, 1024, MAX_QUOTA_GB]) {
      expect(bytesToGb(gbToBytes(gb))).toBe(gb);
    }
  });

  it('keeps whole bytes', () => {
    expect(Number.isInteger(gbToBytes(1.3))).toBe(true);
  });
});

describe('megabits and bits per second', () => {
  it('uses the decimal base link speeds are quoted in', () => {
    expect(mbitToBps(1)).toBe(BPS_PER_MBIT);
    expect(mbitToBps(100)).toBe(100_000_000);
  });

  it('round-trips', () => {
    for (const mbit of [0.5, 1, 2.5, 10, 100, 1000]) {
      expect(bpsToMbit(mbitToBps(mbit))).toBe(mbit);
    }
  });

  it('treats zero and negatives as no limit', () => {
    expect(mbitToBps(0)).toBe(0);
    expect(bpsToMbit(-5)).toBe(0);
  });
});

describe('parseAmount', () => {
  it('reads an empty or blank field as no limit', () => {
    expect(parseAmount('')).toBe(0);
    expect(parseAmount('   ')).toBe(0);
  });

  it('accepts a comma decimal separator', () => {
    expect(parseAmount('1,5')).toBe(1.5);
  });

  it('rejects nonsense and negatives', () => {
    expect(parseAmount('abc')).toBe(0);
    expect(parseAmount('-3')).toBe(0);
  });
});

describe('form <-> api limits', () => {
  it('shows unset limits as empty fields', () => {
    expect(telemtLimitsToForm({})).toEqual(EMPTY_TELEMT_LIMITS_FORM);
    expect(telemtLimitsToForm(undefined)).toEqual(EMPTY_TELEMT_LIMITS_FORM);
    expect(telemtLimitsToForm({ data_quota_bytes: 0, max_tcp_conns: 0 })).toEqual(EMPTY_TELEMT_LIMITS_FORM);
  });

  it('round-trips a full set of limits through the form', () => {
    const limits = {
      data_quota_bytes: gbToBytes(50),
      rate_limit_up_bps: mbitToBps(20),
      rate_limit_down_bps: mbitToBps(100),
      max_unique_ips: 3,
      max_tcp_conns: 64,
    };
    expect(telemtLimitsFromForm(telemtLimitsToForm(limits))).toEqual(limits);
  });

  it('turns empty fields into zeroes the API reads as no limit', () => {
    expect(telemtLimitsFromForm(EMPTY_TELEMT_LIMITS_FORM)).toEqual({
      data_quota_bytes: 0,
      rate_limit_up_bps: 0,
      rate_limit_down_bps: 0,
      max_unique_ips: 0,
      max_tcp_conns: 0,
    });
  });

  it('truncates counts that must be whole', () => {
    const out = telemtLimitsFromForm({ ...EMPTY_TELEMT_LIMITS_FORM, max_unique_ips: '3.7' });
    expect(out.max_unique_ips).toBe(3);
  });
});

describe('isEmptyTelemtLimits', () => {
  it('is true for an absent or all-zero object', () => {
    expect(isEmptyTelemtLimits(undefined)).toBe(true);
    expect(isEmptyTelemtLimits({})).toBe(true);
    expect(isEmptyTelemtLimits({ data_quota_bytes: 0 })).toBe(true);
  });

  it('is false as soon as one limit is set', () => {
    expect(isEmptyTelemtLimits({ max_tcp_conns: 8 })).toBe(false);
  });
});

describe('validateTelemtLimitsForm', () => {
  it('accepts empty and valid input', () => {
    expect(validateTelemtLimitsForm(EMPTY_TELEMT_LIMITS_FORM)).toEqual({});
    expect(validateTelemtLimitsForm({ ...EMPTY_TELEMT_LIMITS_FORM, quota_gb: '1,5', max_tcp_conns: '64' })).toEqual({});
  });

  it('rejects negatives and nonsense', () => {
    expect(validateTelemtLimitsForm({ ...EMPTY_TELEMT_LIMITS_FORM, rate_up_mbit: '-2' })).toEqual({
      rate_up_mbit: 'limit_positive',
    });
    expect(validateTelemtLimitsForm({ ...EMPTY_TELEMT_LIMITS_FORM, quota_gb: 'ten' })).toEqual({ quota_gb: 'limit_positive' });
  });

  it('rejects a fractional count', () => {
    expect(validateTelemtLimitsForm({ ...EMPTY_TELEMT_LIMITS_FORM, max_unique_ips: '2.5' })).toEqual({
      max_unique_ips: 'limit_integer',
    });
  });

  it('holds the quota to the backend ceiling', () => {
    expect(validateTelemtLimitsForm({ ...EMPTY_TELEMT_LIMITS_FORM, quota_gb: String(MAX_QUOTA_GB) })).toEqual({});
    expect(validateTelemtLimitsForm({ ...EMPTY_TELEMT_LIMITS_FORM, quota_gb: String(MAX_QUOTA_GB + 1) })).toEqual({
      quota_gb: 'limit_quota_max',
    });
  });

  // M5: both counters cross the wire as 32-bit unsigned integers, so a value past 2^32-1
  // wraps - 4294967297 would become 1, locking the key out instead of leaving it unlimited.
  it('holds the counters to the backend ceiling', () => {
    expect(validateTelemtLimitsForm({ ...EMPTY_TELEMT_LIMITS_FORM, max_unique_ips: String(MAX_TELEMT_COUNTER) })).toEqual({});
    expect(validateTelemtLimitsForm({ ...EMPTY_TELEMT_LIMITS_FORM, max_unique_ips: String(MAX_TELEMT_COUNTER + 1) })).toEqual({
      max_unique_ips: 'limit_counter_max',
    });
    expect(validateTelemtLimitsForm({ ...EMPTY_TELEMT_LIMITS_FORM, max_tcp_conns: '4294967297' })).toEqual({
      max_tcp_conns: 'limit_counter_max',
    });
  });
});
