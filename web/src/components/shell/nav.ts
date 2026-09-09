import type { LucideIcon } from 'lucide-react';
import { Activity, KeyRound, LayoutDashboard, LayoutTemplate, ScrollText, Server, Settings2 } from 'lucide-react';

/** Which live counter, if any, the sidebar shows on the right of an item. */
export type NavCount = 'nodes' | 'keys';

export interface NavItem {
  to: string;
  icon: LucideIcon;
  labelKey: string;
  /** Matches only the exact path (used for both NavLink `end` and breadcrumb lookup). */
  end?: boolean;
  count?: NavCount;
}

export interface NavGroup {
  labelKey: string;
  items: NavItem[];
}

export const NAV_GROUPS: NavGroup[] = [
  {
    labelKey: 'nav.group_overview',
    items: [
      { to: '/', icon: LayoutDashboard, labelKey: 'nav.dashboard', end: true },
      { to: '/monitoring', icon: Activity, labelKey: 'nav.monitoring' },
    ],
  },
  {
    labelKey: 'nav.group_infrastructure',
    items: [
      { to: '/nodes', icon: Server, labelKey: 'nav.nodes', count: 'nodes' },
      { to: '/sites', icon: LayoutTemplate, labelKey: 'nav.sites' },
    ],
  },
  {
    labelKey: 'nav.group_access',
    items: [{ to: '/keys', icon: KeyRound, labelKey: 'nav.keys', count: 'keys' }],
  },
  {
    labelKey: 'nav.group_system',
    items: [
      { to: '/audit', icon: ScrollText, labelKey: 'nav.audit' },
      { to: '/settings', icon: Settings2, labelKey: 'nav.settings' },
    ],
  },
];

/** Flat list in rail order - used by the breadcrumb and the command palette. */
export const NAV_ITEMS: NavItem[] = NAV_GROUPS.flatMap((g) => g.items);

/** Finds the nav section a given pathname belongs to, for the topbar breadcrumb. */
export function navItemForPath(pathname: string): NavItem | undefined {
  if (pathname === '/') return NAV_ITEMS.find((i) => i.to === '/');
  return [...NAV_ITEMS].reverse().find((item) => item.to !== '/' && pathname.startsWith(item.to));
}
