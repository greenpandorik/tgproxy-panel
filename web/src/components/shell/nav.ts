import type { TFunction } from 'i18next';
import type { LucideIcon } from 'lucide-react';
import { Activity, FileText, Globe, LayoutDashboard, LayoutTemplate, ScrollText, Server, Settings2, Users } from 'lucide-react';

export interface NavItem {
  to: string;
  icon: LucideIcon;
  labelKey: string;
  /** Matches only the exact path (used for both NavLink `end` and breadcrumb lookup). */
  end?: boolean;
}

export interface NavGroup {
  labelKey: string;
  items: NavItem[];
  qualified?: boolean;
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
      { to: '/nodes', icon: Server, labelKey: 'nav.nodes' },
      { to: '/sites', icon: LayoutTemplate, labelKey: 'nav.sites' },
    ],
  },
  {
    labelKey: 'nav.group_access',
    items: [{ to: '/users', icon: Users, labelKey: 'nav.users' }],
  },
  {
    labelKey: 'nav.group_subscription',
    qualified: true,
    items: [
      { to: '/subscription', icon: FileText, labelKey: 'nav.subscription_page', end: true },
      { to: '/subscription/service', icon: Globe, labelKey: 'nav.subscription_service' },
    ],
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

export function navGroupOf(item: NavItem): NavGroup | undefined {
  return NAV_GROUPS.find((group) => group.items.includes(item));
}

export function navLabel(item: NavItem, t: TFunction): string {
  const group = navGroupOf(item);
  return group?.qualified ? `${t(group.labelKey)} · ${t(item.labelKey)}` : t(item.labelKey);
}

/** Finds the nav section a given pathname belongs to, for the topbar breadcrumb. */
export function navItemForPath(pathname: string): NavItem | undefined {
  if (pathname === '/') return NAV_ITEMS.find((i) => i.to === '/');
  return [...NAV_ITEMS].reverse().find((item) => item.to !== '/' && pathname.startsWith(item.to));
}
