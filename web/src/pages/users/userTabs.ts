export const USER_TABS = ['main', 'access', 'limits', 'stats'] as const;
export type UserTab = (typeof USER_TABS)[number];

const FIELD_TAB: Record<string, UserTab> = {
  label: 'main',
  owner_label: 'main',
  note: 'main',
  sub_slug: 'main',
  expires_at: 'main',
  no_expiry: 'main',
  node_ids: 'access',
  carrier_mode: 'access',
  limits: 'limits',
  telemt_limits: 'limits',
};

/** The tab a field lives on, whether the form or the server names it (`limits.max_streams`, `telemt_limits`). */
export function tabOfField(field: string): UserTab | undefined {
  return FIELD_TAB[field.split('.')[0]];
}

/** Every tab holding at least one of the given error keys. */
export function tabsWithErrors(fields: Iterable<string>): Set<UserTab> {
  const out = new Set<UserTab>();
  for (const field of fields) {
    const tab = tabOfField(field);
    if (tab) out.add(tab);
  }
  return out;
}

/** Where a failed save takes the reader: the open tab if it has an error, otherwise the first tab that does. */
export function tabForErrors(current: UserTab, tabs: Set<UserTab>): UserTab {
  if (tabs.has(current)) return current;
  return USER_TABS.find((tab) => tabs.has(tab)) ?? current;
}
