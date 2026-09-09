import type { StatTone } from './statTone';

/** The panel's whole status vocabulary. Nothing renders a status outside this list. */
export type HealthStatus = 'healthy' | 'degraded' | 'offline' | 'installing' | 'updating' | 'draining' | 'unknown';

export const HEALTH_STATUSES = [
  'healthy',
  'degraded',
  'offline',
  'installing',
  'updating',
  'draining',
  'unknown',
] as const satisfies readonly HealthStatus[];

export const HEALTH_TONE: Record<HealthStatus, StatTone> = {
  healthy: 'ok',
  degraded: 'warn',
  offline: 'err',
  installing: 'info',
  updating: 'info',
  draining: 'warn',
  unknown: 'neutral',
};
