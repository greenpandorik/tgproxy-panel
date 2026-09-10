import type { WebRuntime } from '@/api/types';
import type { HealthStatus } from '@/components/common/healthStatus';

/** telemt's operator lifecycle states, matching internal/telemt.WebLifecycle*. */
export const WEB_LIFECYCLE_STATES = ['running', 'paused', 'draining', 'force_closing', 'drained'] as const;

const LIFECYCLE_STATUS: Record<string, HealthStatus> = {
  running: 'healthy',
  paused: 'degraded',
  draining: 'draining',
  force_closing: 'draining',
  drained: 'offline',
};

/**
 * The WEB transport's own status. A node that never reported a runtime is unknown, not
 * stopped: the panel has no reading, and says so rather than drawing a red light.
 */
export function webRuntimeStatus(runtime: WebRuntime | null | undefined, offline: boolean): HealthStatus {
  if (offline) return 'offline';
  if (!runtime) return 'unknown';
  const state = runtime.lifecycle?.state;
  if (!state) return 'healthy';
  return LIFECYCLE_STATUS[state] ?? 'unknown';
}
