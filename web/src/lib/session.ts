import type { QueryClient } from '@tanstack/react-query';

const revisions = new WeakMap<QueryClient, number>();
const meKey = ['auth', 'me'] as const;

export function advanceSession(qc: QueryClient) {
  revisions.set(qc, (revisions.get(qc) ?? 0) + 1);
}

/** Capture before asynchronous work; use before writing its results or rolling back. */
export function sessionGuard(qc: QueryClient) {
  const revision = revisions.get(qc) ?? 0;
  const identity = () => {
    const me = qc.getQueryData<{ id: string; role: string } | null>(meKey);
    return me ? `${me.id}:${me.role}` : null;
  };
  const owner = identity();
  return () => revision === (revisions.get(qc) ?? 0) && owner === identity();
}
