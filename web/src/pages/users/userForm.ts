import { z } from 'zod';

import { EMPTY_TELEMT_LIMITS_FORM, telemtLimitsFromForm, telemtLimitsToForm, validateTelemtLimitsForm } from '@/lib/units';

import { ZERO_LIMITS } from './LimitsFields';

import type { AccessKey, KeyInput, PatchKeyInput } from '@/api/types';
import type { TelemtLimitsForm } from '@/lib/units';

export const SLUG_RE = /^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$/;

const limitsShape = {
  max_sessions: z.number().int().min(0),
  max_streams: z.number().int().min(0),
  max_backend_dials_in_flight: z.number().int().min(0),
  new_sessions_per_minute: z.number().int().min(0),
  new_sessions_burst: z.number().int().min(0),
  new_streams_per_minute: z.number().int().min(0),
  new_streams_burst: z.number().int().min(0),
  max_streams_per_session: z.number().int().min(0),
  max_pending_per_session: z.number().int().min(0),
} as const;

export const userSchema = z
  .object({
    creating: z.boolean(),
    mode: z.enum(['personal', 'shared', 'batch']),
    label: z.string(),
    owner_label: z.string(),
    note: z.string(),
    sub_slug: z.string(),
    prefix: z.string(),
    count: z.coerce.number().int(),
    carrier_mode: z.enum(['https', 'https-lanes', 'websocket', 'websocket-lanes']),
    node_ids: z.array(z.string()),
    no_expiry: z.boolean(),
    expires_at: z.string(),
    limits: z.object(limitsShape),
    telemt_limits: z.object({
      quota_gb: z.string(),
      rate_up_mbit: z.string(),
      rate_down_mbit: z.string(),
      max_unique_ips: z.string(),
      max_tcp_conns: z.string(),
    }),
  })
  .superRefine((val, ctx) => {
    if (val.mode !== 'batch' && val.label.trim() === '') {
      ctx.addIssue({ code: 'custom', path: ['label'], message: 'required' });
    }
    if (val.mode === 'batch') {
      if (val.prefix.trim() === '') ctx.addIssue({ code: 'custom', path: ['prefix'], message: 'required' });
      if (!Number.isFinite(val.count) || val.count < 1 || val.count > 100) {
        ctx.addIssue({ code: 'custom', path: ['count'], message: 'range' });
      }
    }
    if (val.mode === 'shared' && val.sub_slug.trim() !== '' && !SLUG_RE.test(val.sub_slug.trim())) {
      ctx.addIssue({ code: 'custom', path: ['sub_slug'], message: 'slug' });
    }
    if (val.node_ids.length === 0) {
      ctx.addIssue({ code: 'custom', path: ['node_ids'], message: 'required' });
    }
    if (!val.no_expiry) {
      const d = new Date(val.expires_at);
      if (val.expires_at.trim() === '' || Number.isNaN(d.getTime())) {
        ctx.addIssue({ code: 'custom', path: ['expires_at'], message: 'required' });
      } else if (val.creating && d.getTime() <= Date.now()) {
        ctx.addIssue({ code: 'custom', path: ['expires_at'], message: 'future' });
      }
    }
    const l = val.limits;
    if (l.max_streams > 0 && l.max_streams_per_session > l.max_streams) {
      ctx.addIssue({ code: 'custom', path: ['limits', 'max_streams_per_session'], message: 'exceeds_max_streams' });
    }
    if (l.max_streams > 0 && l.max_backend_dials_in_flight > l.max_streams) {
      ctx.addIssue({ code: 'custom', path: ['limits', 'max_backend_dials_in_flight'], message: 'exceeds_max_streams' });
    }
    for (const [name, message] of Object.entries(validateTelemtLimitsForm(val.telemt_limits))) {
      ctx.addIssue({ code: 'custom', path: ['telemt_limits', name], message });
    }
  });

export type UserFormValues = z.infer<typeof userSchema>;

export const NEW_USER: UserFormValues = {
  creating: true,
  mode: 'personal',
  label: '',
  owner_label: '',
  note: '',
  sub_slug: '',
  prefix: '',
  count: 5,
  carrier_mode: 'https',
  node_ids: [],
  no_expiry: true,
  expires_at: '',
  limits: { ...ZERO_LIMITS },
  telemt_limits: { ...EMPTY_TELEMT_LIMITS_FORM },
};

/** Formats a date as the local "YYYY-MM-DDTHH:mm" value an <input type="datetime-local"> expects. */
export function toLocalInputValue(value: string | Date | null): string {
  if (!value) return '';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export type ExpiryStep = '1m' | '3m' | '1y';

/** Moves an expiry forward from its current value, or from now when it has none or has passed. */
export function extendExpiry(current: string, step: ExpiryStep): string {
  const parsed = current ? new Date(current) : null;
  const base = parsed && !Number.isNaN(parsed.getTime()) && parsed.getTime() > Date.now() ? parsed : new Date();
  const next = new Date(base);
  if (step === '1m') next.setMonth(next.getMonth() + 1);
  if (step === '3m') next.setMonth(next.getMonth() + 3);
  if (step === '1y') next.setFullYear(next.getFullYear() + 1);
  if (!parsed || parsed.getTime() <= Date.now()) next.setHours(23, 59, 0, 0);
  return toLocalInputValue(next);
}

export function valuesFromUser(key: AccessKey): UserFormValues {
  return {
    creating: false,
    mode: key.type === 'SHARED' ? 'shared' : 'personal',
    label: key.label,
    owner_label: key.owner_label,
    note: key.note,
    sub_slug: key.sub_slug ?? '',
    prefix: '',
    count: 1,
    carrier_mode: key.carrier_mode,
    node_ids: key.nodes.map((n) => n.node_id),
    no_expiry: !key.expires_at,
    expires_at: toLocalInputValue(key.expires_at),
    limits: { ...ZERO_LIMITS, ...key.limits },
    telemt_limits: telemtLimitsToForm(key.telemt_limits),
  };
}

function expiryISO(v: UserFormValues): string | undefined {
  return v.no_expiry ? undefined : new Date(v.expires_at).toISOString();
}

export function createPayload(v: UserFormValues, telemtOnly: boolean): KeyInput {
  const payload: KeyInput = {
    label: v.mode === 'batch' ? v.prefix.trim() : v.label.trim(),
    type: v.mode === 'shared' ? 'SHARED' : 'PERSONAL',
    owner_label: v.mode === 'batch' ? undefined : v.owner_label.trim() || undefined,
    note: v.note.trim() || undefined,
    carrier_mode: telemtOnly ? 'https' : v.carrier_mode,
    limits: { ...v.limits },
    telemt_limits: telemtLimitsFromForm(v.telemt_limits),
    expires_at: expiryISO(v),
    node_ids: v.node_ids,
  };
  if (v.mode === 'batch') {
    payload.prefix = v.prefix.trim();
    payload.count = v.count;
  }
  if (v.mode === 'shared' && v.sub_slug.trim()) payload.sub_slug = v.sub_slug.trim();
  return payload;
}

export function patchPayload(v: UserFormValues): PatchKeyInput {
  const out: PatchKeyInput = {
    label: v.label.trim(),
    owner_label: v.owner_label.trim(),
    note: v.note.trim(),
    carrier_mode: v.carrier_mode,
    limits: { ...v.limits },
    telemt_limits: telemtLimitsFromForm(v.telemt_limits),
    clear_expiry: v.no_expiry,
    expires_at: expiryISO(v),
  };
  if (v.mode === 'shared') out.sub_slug = v.sub_slug.trim();
  return out;
}

export type TelemtErrors = Partial<Record<keyof TelemtLimitsForm, string>>;
