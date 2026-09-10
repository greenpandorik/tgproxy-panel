import { CARRIERS } from '@/api/types';
import { isMetricPresent } from '@/components/common/metric';

import type { Carrier, WebCarrierShare, WebCarrierStats } from '@/api/types';

/** One colour per carrier, fixed so the same carrier keeps its hue on every surface. */
export const CARRIER_COLOR: Record<Carrier, string> = {
  'websocket-lanes': '#a78bfa',
  websocket: '#38bdf8',
  'https-lanes': '#f59e0b',
  https: '#22c55e',
};

/** The carrier names the key dialogs already use, so one carrier is named once in the panel. */
export const CARRIER_I18N_KEY: Record<Carrier, string> = {
  'websocket-lanes': 'keys.carrier_websocket_lanes',
  websocket: 'keys.carrier_websocket',
  'https-lanes': 'keys.carrier_https_lanes',
  https: 'keys.carrier_https',
};

export function isCarrier(value: string): value is Carrier {
  return (CARRIERS as string[]).includes(value);
}

/** One row of the distribution: a carrier, and either its measured slice or nothing at all. */
export interface CarrierRow {
  carrier: Carrier;
  color: string;
  /** Null when no node reported this carrier over the window. Absent, never zero. */
  selections: number | null;
  /** Null for the same reason. A present 0 is a real "chosen no times". */
  share: number | null;
}

/**
 * The distribution in the panel's fixed carrier order, keeping absence intact: a carrier the
 * API returned as null stays null, and one it did not return at all is null too.
 */
export function carrierRows(distribution: WebCarrierShare[] | undefined): CarrierRow[] {
  const byCarrier = new Map<string, WebCarrierShare>();
  for (const row of distribution ?? []) byCarrier.set(row.carrier, row);

  return CARRIERS.map((carrier) => {
    const row = byCarrier.get(carrier);
    return {
      carrier,
      color: CARRIER_COLOR[carrier],
      selections: isMetricPresent(row?.selections) ? row.selections : null,
      share: isMetricPresent(row?.share) ? row.share : null,
    };
  });
}

/** True when not one carrier in the window carried a measurement. */
export function distributionIsAbsent(rows: CarrierRow[]): boolean {
  return rows.every((row) => row.share === null);
}

/** The rows that have a share to draw, largest first - what the bar is made of. */
export function drawableRows(rows: CarrierRow[]): (CarrierRow & { share: number })[] {
  return rows
    .filter((row): row is CarrierRow & { share: number } => row.share !== null && row.share > 0)
    .sort((a, b) => b.share - a.share);
}

export function formatShare(share: number, locale?: string): string {
  return new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 1 }).format(share);
}

/** The counters the panel shows beside the distribution, in display order. */
export const WEB_COUNTERS = [
  { id: 'carrier_failures', labelKey: 'web.counter_carrier_failures', hintKey: 'web.counter_carrier_failures_hint' },
  { id: 'rejected_attempts', labelKey: 'web.counter_rejected_attempts', hintKey: 'web.counter_rejected_attempts_hint' },
  { id: 'evicted_sessions', labelKey: 'web.counter_evicted_sessions', hintKey: 'web.counter_evicted_sessions_hint' },
  { id: 'bridge_recoveries', labelKey: 'web.counter_bridge_recoveries', hintKey: 'web.counter_bridge_recoveries_hint' },
] as const satisfies readonly { id: keyof WebCarrierStats; labelKey: string; hintKey: string }[];
