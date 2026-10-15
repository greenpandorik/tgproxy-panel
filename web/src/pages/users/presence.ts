import type { AccessKey, KeyPresenceNode } from '@/api/types';

/** Each server's part of the people it counts, as a fraction; null when nobody is counted. */
export function nodeShares(nodes: KeyPresenceNode[]): (number | null)[] {
  const total = nodes.reduce((sum, n) => sum + n.people, 0);
  return nodes.map((n) => (total > 0 ? n.people / total : null));
}

/** People online on a user right now, the way the list counts them. */
export function peopleNow(user: AccessKey): number {
  return user.live.online ? user.live.devices : 0;
}

/** Shared users by people online right now, most first; revoked ones are left out. */
export function sharedByPeople(users: AccessKey[]): AccessKey[] {
  return users.filter((u) => u.state !== 'revoked').sort((a, b) => peopleNow(b) - peopleNow(a) || a.label.localeCompare(b.label));
}
