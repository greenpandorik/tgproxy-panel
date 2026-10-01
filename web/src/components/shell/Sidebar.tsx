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

import { NAV_GROUPS, navLabel } from './nav';

import type { NavItem } from './nav';

interface SidebarProps {
  /** Icon-only rail (desktop, user-collapsed). Drawer variant ignores this and always shows labels. */
  collapsed?: boolean;
  /** Mobile Sheet: its close button floats in the header corner. */
  inDrawer?: boolean;
  onNavigate?: () => void;
  onToggleCollapsed?: () => void;
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

function NavRow({ item, collapsed, onNavigate }: { item: NavItem; collapsed: boolean; onNavigate?: () => void }) {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  const label = t(item.labelKey);
  const fullLabel = navLabel(item, t);
  const isActive = item.end ? pathname === item.to : pathname === item.to || pathname.startsWith(`${item.to}/`);

  const link = (
    <Link
      to={item.to}
      onClick={onNavigate}
      aria-label={collapsed ? fullLabel : undefined}
      aria-current={isActive ? 'page' : undefined}
      className={cn(
        'panel-nav-link relative flex h-10 items-center gap-3 rounded-control px-3 text-body transition-[background-color,border-color,color,scale] duration-fast ease-out focus-visible:-outline-offset-2 active:scale-[0.985]',
        collapsed && 'justify-center px-0',
        !isActive && 'text-sidebar-foreground/85 hover:bg-elevated/60 hover:text-foreground',
      )}
    >
      <item.icon className="size-[18px] shrink-0" strokeWidth={1.8} aria-hidden="true" />
      {!collapsed && <span className="truncate">{label}</span>}
    </Link>
  );

  if (!collapsed) return <li>{link}</li>;

  return (
    <li>
      <Tooltip>
        <TooltipTrigger render={link} />
        <TooltipContent side="right">{fullLabel}</TooltipContent>
      </Tooltip>
    </li>
  );
}

export function Sidebar({ collapsed = false, inDrawer = false, onNavigate, onToggleCollapsed }: SidebarProps) {
  const { t } = useTranslation();
  const { branding, theme } = useBrandingIdentity();
  const toggle =
    inDrawer || !onToggleCollapsed ? null : (
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              onClick={onToggleCollapsed}
              aria-label={t(collapsed ? 'shell.expand_sidebar' : 'shell.collapse_sidebar')}
              className="inline-flex size-8 shrink-0 items-center justify-center rounded-control border border-hairline-strong bg-elevated text-muted-foreground transition-colors hover:border-mute/60 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
            />
          }
        >
          {collapsed ? (
            <PanelLeftOpen className="size-4" aria-hidden="true" />
          ) : (
            <PanelLeftClose className="size-4" aria-hidden="true" />
          )}
        </TooltipTrigger>
        <TooltipContent side="right">{t(collapsed ? 'shell.expand_sidebar' : 'shell.collapse_sidebar')}</TooltipContent>
      </Tooltip>
    );
  const healthQuery = usePanelHealth();
  const clock = useClock();
  const apiOk = healthQuery.data === true && !healthQuery.isError;

  return (
    <nav
      className="wave-sidebar flex h-full flex-col px-3 py-4 text-sidebar-foreground"
      aria-label={t('shell.primary_navigation')}
    >
      <div
        className={cn(
          'flex items-center gap-3 border-b border-hairline px-2 pt-1 pb-4',
          collapsed && 'flex-col justify-center gap-3 px-0',
          inDrawer && 'pr-7',
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
          <span
            className="min-w-0 flex-1 truncate text-title font-semibold tracking-tight"
            title={branding?.panel_name || DEFAULT_PANEL_NAME}
          >
            {branding?.panel_name || DEFAULT_PANEL_NAME}
          </span>
        )}
        {toggle}
      </div>

      <div className="flex-1 overflow-x-hidden overflow-y-auto">
        {NAV_GROUPS.map((group, index) => (
          <div key={group.labelKey}>
            {index > 0 && <div className={cn('wave-rule mt-3', collapsed ? 'mx-2 mb-3' : 'mx-1')} aria-hidden="true" />}
            {!collapsed && (
              <p className="wave-group-label flex items-center gap-2 px-3 pt-3 pb-1.5 text-[11px] leading-4 font-semibold tracking-[0.12em] text-mute uppercase">
                {t(group.labelKey)}
              </p>
            )}
            <ul className="space-y-1">
              {group.items.map((item) => (
                <NavRow key={item.to} item={item} collapsed={collapsed} onNavigate={onNavigate} />
              ))}
            </ul>
          </div>
        ))}
      </div>

      <div className="wave-rule mt-4 pt-3">
        <div className={cn('mono min-w-0 text-micro text-mute', collapsed ? 'text-center' : 'px-2')}>
          {collapsed ? (
            <span
              className={cn('inline-block size-1.5 rounded-pill', apiOk ? 'bg-ok' : 'bg-err')}
              aria-label={apiOk ? 'ok' : 'error'}
            />
          ) : (
            <>
              <p className="truncate pt-1">
                {healthQuery.isLoading
                  ? t('common.state.loading')
                  : apiOk
                    ? t('shell.panel_available')
                    : t('shell.panel_unavailable')}{' '}
                · {clock}
              </p>
            </>
          )}
        </div>
      </div>

      {!collapsed && branding?.footer_text && (
        <p className="truncate px-2 pt-2 text-label text-mute" title={branding.footer_text}>
          {branding.footer_text}
        </p>
      )}
    </nav>
  );
}
