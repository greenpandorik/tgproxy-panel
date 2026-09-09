import { PanelLeftClose, PanelLeftOpen } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useLocation } from 'react-router-dom';

import { useBrandingIdentity } from '@/theme/ThemeProvider';
import { usePanelHealth } from '@/api/health';
import { DEFAULT_PANEL_NAME } from '@/components/brand/brand';
import { Logo } from '@/components/brand/Logo';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';

import { NAV_GROUPS } from './nav';

import type { NavItem } from './nav';

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
      aria-label={collapsed ? label : undefined}
      aria-current={isActive ? 'page' : undefined}
      className={cn(
        'panel-nav-link relative flex h-11 items-center gap-3 rounded-control px-3 text-body transition-[background-color,color,scale] duration-fast ease-out active:scale-[0.985]',
        collapsed && 'justify-center px-0',
        isActive ? 'bg-elevated text-brand-ink font-medium' : 'text-muted-foreground hover:bg-elevated/60 hover:text-foreground',
      )}
    >
      <item.icon className="size-[18px] shrink-0" strokeWidth={1.8} aria-hidden="true" />
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
  const { branding, theme } = useBrandingIdentity();
  const healthQuery = usePanelHealth();
  const clock = useClock();
  const apiOk = healthQuery.data === true && !healthQuery.isError;
  // The summary query is the cheapest thing on the page that actually reads
  // postgres, so its error state is an honest proxy for "db reachable".

  return (
    <nav
      className="flex h-full flex-col bg-sidebar px-2.5 py-3 text-sidebar-foreground"
      aria-label={t('shell.primary_navigation')}
    >
      {/* The drawer (no rail toggle) has the Sheet's own close button floating in
          this corner, so the version chip keeps clear of it. */}
      <div
        className={cn(
          'flex items-center gap-3 px-2 pt-1 pb-5',
          collapsed && 'justify-center px-0',
          !showToggle && !collapsed && 'pr-7',
        )}
      >
        {(theme === 'dark' ? branding?.logo_dark_url || branding?.logo_url : branding?.logo_url) ? (
          <img
            src={theme === 'dark' ? branding?.logo_dark_url || branding?.logo_url : branding?.logo_url}
            alt=""
            className="size-8 shrink-0 object-contain"
          />
        ) : (
          <Logo size={24} />
        )}
        {!collapsed && (
          <>
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
              <p className="px-3 pt-5 pb-2 text-label font-medium text-mute">{t(group.labelKey)}</p>
            )}
            <ul className="space-y-1">
              {group.items.map((item) => (
                <NavRow key={item.to} item={item} collapsed={collapsed} onNavigate={onNavigate} />
              ))}
            </ul>
          </div>
        ))}
      </div>

      <div className="mt-4 flex items-end gap-1 border-t border-hairline pt-3">
        <div className={cn('mono min-w-0 flex-1 text-micro text-mute', collapsed ? 'text-center' : 'px-2')}>
          {collapsed ? (
            <span
              className={cn('inline-block size-1.5 rounded-pill', apiOk ? 'bg-ok' : 'bg-err')}
              aria-label={apiOk ? 'ok' : 'error'}
            />
          ) : (
            <>
              <p className="truncate pt-1">
                {healthQuery.isLoading ? t('common.loading') : apiOk ? t('shell.panel_available') : t('shell.panel_unavailable')}{' '}
                · {clock}
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
                  className="flex size-9 shrink-0 items-center justify-center rounded-control text-mute transition-[background-color,color,scale] duration-fast ease-out hover:bg-elevated hover:text-foreground active:scale-[0.985]"
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
                className="mt-2 flex h-10 w-full items-center justify-center rounded-control text-mute transition-[background-color,color,scale] duration-fast ease-out hover:bg-elevated hover:text-foreground active:scale-[0.985]"
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
