import { PanelLeftClose, PanelLeftOpen } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useLocation } from 'react-router-dom';

import { useBranding } from '@/api/branding';
import { useDashboardSummary } from '@/api/dashboard';
import { usePanelHealth } from '@/api/health';
import { useNodes } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { DEFAULT_PANEL_NAME } from '@/components/brand/brand';
import { Logo } from '@/components/brand/Logo';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';

import { NAV_GROUPS } from './nav';

import type { NavItem } from './nav';

const ROLE_KEY: Record<string, string> = {
  owner: 'common.role_owner',
  admin: 'common.role_admin',
  viewer: 'common.role_viewer',
};

interface SidebarProps {
  /** Icon-only rail (desktop, user-collapsed). Drawer variant ignores this and always shows labels. */
  collapsed?: boolean;
  /** Rail shows the collapse toggle; drawer (mobile Sheet) does not. */
  showToggle?: boolean;
  onToggle?: () => void;
  onNavigate?: () => void;
}

/** `HH:MM`, re-rendered once a minute - the footer's "as of" stamp. */
function useClock(): string {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = window.setInterval(() => setNow(new Date()), 30_000);
    return () => window.clearInterval(id);
  }, []);
  return `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
}

/**
 * One rail row. Active state is derived from the pathname rather than via
 * NavLink's render-prop className, because the collapsed rail wraps the same
 * element in a Tooltip trigger and that needs a plain string class to merge.
 */
function NavRow({
  item,
  count,
  collapsed,
  onNavigate,
}: {
  item: NavItem;
  count?: string;
  collapsed: boolean;
  onNavigate?: () => void;
}) {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  const label = t(item.labelKey);
  const isActive = item.end ? pathname === item.to : pathname === item.to || pathname.startsWith(`${item.to}/`);

  const link = (
    <Link
      to={item.to}
      onClick={onNavigate}
      aria-current={isActive ? 'page' : undefined}
      className={cn(
        'relative flex h-8 items-center gap-2.5 rounded-control px-2 text-body transition-[background-color,color,scale] duration-fast ease-out active:scale-[0.985]',
        collapsed && 'justify-center px-0',
        isActive
          ? // The one place the operator's brand hue marks navigation:
            // a 2px tab on the left edge of the active row. It is a rule, not a
            // control, so it takes the zero radius rules and hairlines take.
            'bg-elevated text-foreground before:absolute before:top-1.5 before:bottom-1.5 before:left-0 before:w-0.5 before:bg-brand-primary before:content-[""]'
          : 'text-muted-foreground hover:bg-elevated/60 hover:text-foreground',
      )}
    >
      <item.icon className="size-[15px] shrink-0" strokeWidth={1.8} aria-hidden="true" />
      {!collapsed && <span className="truncate">{label}</span>}
      {!collapsed && count && <span className="mono ml-auto text-micro text-mute">{count}</span>}
    </Link>
  );

  if (!collapsed) return <li>{link}</li>;

  return (
    <li>
      <Tooltip>
        <TooltipTrigger render={link} />
        <TooltipContent side="right">
          {label}
          {count ? <span className="mono text-mute"> {count}</span> : null}
        </TooltipContent>
      </Tooltip>
    </li>
  );
}

export function Sidebar({ collapsed = false, showToggle = false, onToggle, onNavigate }: SidebarProps) {
  const { t } = useTranslation();
  const { data: branding } = useBranding();
  const { user } = useAuth();
  const summaryQuery = useDashboardSummary();
  const healthQuery = usePanelHealth();
  // Same cached query the palette and the nodes page read, so the rail costs
  // no extra request - and, more to the point, counts the same field they
  // display (`status`) instead of the server's separate tally.
  const nodesQuery = useNodes();
  const clock = useClock();

  const summary = summaryQuery.data;
  const nodes = nodesQuery.data?.items;
  const counts: Record<string, string | undefined> = {
    // Online-ness is read from the persisted status, never from the driver's
    // live probe (see nodeDisplay.nodeStatus): the rail and the nodes page
    // must not print two different numbers for the same fleet.
    nodes: nodes ? `${nodes.filter((n) => n.status === 'online').length}/${nodes.length}` : undefined,
    keys: summary ? String(summary.keys.active) : undefined,
  };

  const apiOk = healthQuery.data !== false && !healthQuery.isError;
  // The summary query is the cheapest thing on the page that actually reads
  // postgres, so its error state is an honest proxy for "db reachable".
  const dbOk = !summaryQuery.isError;

  return (
    <nav
      className="flex h-full flex-col bg-sidebar px-2.5 py-3 text-sidebar-foreground"
      aria-label={t('shell.primary_navigation')}
    >
      {/* The drawer (no rail toggle) has the Sheet's own close button floating in
          this corner, so the version chip keeps clear of it. */}
      <div
        className={cn(
          'flex items-center gap-2 px-2 pb-4',
          collapsed && 'justify-center px-0',
          !showToggle && !collapsed && 'pr-7',
        )}
      >
        <Logo size={16} />
        {!collapsed && (
          <>
            {/*
              The name gets the whole header. The version used to sit here as a
              badge, but the topbar chip carries the same number plus the update
              state, and between the two the default name lost 7px and truncated
              to "TGProxy P...". One fact, one place, and the product can say its
              own name.
            */}
            <span className="truncate text-body font-semibold">{branding?.panel_name || DEFAULT_PANEL_NAME}</span>
          </>
        )}
      </div>

      <div className="flex-1 overflow-x-hidden overflow-y-auto">
        {NAV_GROUPS.map((group) => (
          <div key={group.labelKey}>
            {collapsed ? (
              <div className="mx-2 my-2 h-px bg-hairline" aria-hidden="true" />
            ) : (
              <p className="micro px-2 pt-4 pb-1 text-mute">{t(group.labelKey)}</p>
            )}
            <ul>
              {group.items.map((item) => (
                <NavRow
                  key={item.to}
                  item={item}
                  count={item.count ? counts[item.count] : undefined}
                  collapsed={collapsed}
                  onNavigate={onNavigate}
                />
              ))}
            </ul>
          </div>
        ))}
      </div>

      <div className="mt-4 flex items-end gap-1 border-t border-hairline pt-3">
        <div className={cn('mono min-w-0 flex-1 text-micro text-mute', collapsed ? 'text-center' : 'px-2')}>
          {collapsed ? (
            <span
              className={cn('inline-block size-1.5 rounded-pill', apiOk && dbOk ? 'bg-ok' : 'bg-err')}
              aria-label={apiOk && dbOk ? 'ok' : 'error'}
            />
          ) : (
            <>
              <p className="flex items-center gap-1.5 truncate">
                <span className={cn('size-1.5 shrink-0 rounded-pill', apiOk && dbOk ? 'bg-ok' : 'bg-err')} aria-hidden="true" />
                <span className="truncate text-muted-foreground">{user?.username ?? '\u2014'}</span>
                <span aria-hidden="true">·</span>
                <span className="truncate">{user ? t(ROLE_KEY[user.role] ?? 'common.role_viewer') : ''}</span>
              </p>
              <p className="truncate pt-1">
                {t('shell.health_api')} {apiOk ? t('shell.health_ok') : t('shell.health_down')} · {t('shell.health_db')}{' '}
                {dbOk ? t('shell.health_ok') : t('shell.health_down')} · {clock}
              </p>
            </>
          )}
        </div>

        {/* Icon only: the label would be the longest string in the rail. */}
        {showToggle && !collapsed && (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={onToggle}
                  aria-label={t('shell.toggle_sidebar')}
                  className="flex size-6 shrink-0 items-center justify-center rounded-control text-mute transition-[background-color,color,scale] duration-fast ease-out hover:bg-elevated hover:text-foreground active:scale-[0.985]"
                >
                  <PanelLeftClose className="size-3.5" aria-hidden="true" />
                </button>
              }
            />
            <TooltipContent side="right">{t('shell.toggle_sidebar')}</TooltipContent>
          </Tooltip>
        )}
      </div>

      {showToggle && collapsed && (
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={onToggle}
                aria-label={t('shell.toggle_sidebar')}
                className="mt-2 flex h-7 w-full items-center justify-center rounded-control text-mute transition-[background-color,color,scale] duration-fast ease-out hover:bg-elevated hover:text-foreground active:scale-[0.985]"
              >
                <PanelLeftOpen className="size-3.5" aria-hidden="true" />
              </button>
            }
          />
          <TooltipContent side="right">{t('shell.toggle_sidebar')}</TooltipContent>
        </Tooltip>
      )}

      {!collapsed && branding?.footer_text && <p className="truncate px-2 pt-2 text-label text-mute">{branding.footer_text}</p>}
    </nav>
  );
}
