import { LogOut, Menu, Moon, PanelLeftClose, PanelLeftOpen, Search, ShieldCheck, SlidersHorizontal, Sun } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';

import { useAuth } from '@/auth/AuthProvider';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import type { Lang } from '@/i18n';
import { setLang } from '@/i18n';
import { cn } from '@/lib/utils';
import { useTheme } from '@/theme/ThemeProvider';

import { StatusChips, StatusMenuRows } from './StatusChips';

const ROLE_KEY: Record<string, string> = {
  owner: 'common.role_owner',
  admin: 'common.role_admin',
  viewer: 'common.role_viewer',
};

/** One square in the header's control row. The chips next to it carry the same height and radius. */
const SQUARE =
  'size-10 rounded-surface border border-hairline-strong bg-surface text-mute hover:text-foreground pointer-coarse:size-11';

function HeaderButton({
  label,
  onClick,
  className,
  children,
}: {
  label: string;
  onClick: () => void;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={onClick}
            aria-label={label}
            className={cn(SQUARE, className)}
          />
        }
      >
        {children}
      </TooltipTrigger>
      <TooltipContent side="bottom">{label}</TooltipContent>
    </Tooltip>
  );
}

interface TopbarProps {
  onOpenMenu: () => void;
  onOpenCommand: () => void;
}

export function Topbar({ onOpenMenu, onOpenCommand }: TopbarProps) {
  const { t, i18n } = useTranslation();
  const { theme, toggleTheme, collapsed, setCollapsed } = useTheme();
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
  const lang = (i18n.language?.startsWith('en') ? 'en' : 'ru') as Lang;

  return (
    <header className="flex min-h-16 shrink-0 items-center gap-2 px-3 sm:px-6">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className={cn(SQUARE, 'lg:hidden')}
        onClick={onOpenMenu}
        aria-label={t('shell.open_menu')}
      >
        <Menu />
      </Button>

      <HeaderButton
        label={t(collapsed ? 'shell.expand_sidebar' : 'shell.collapse_sidebar')}
        onClick={() => setCollapsed(!collapsed)}
        className="hidden lg:inline-flex"
      >
        {collapsed ? <PanelLeftOpen /> : <PanelLeftClose />}
      </HeaderButton>

      <div className="min-w-0 flex-1" />

      {/* The palette is the only search in the product, so this opens it rather than taking text. */}
      <HeaderButton label={`${t('shell.command_placeholder')} (${isMac ? '⌘K' : 'Ctrl K'})`} onClick={onOpenCommand}>
        <Search />
      </HeaderButton>

      {/* Below lg the row would not fit; the same facts are rows in the menu. */}
      <StatusChips className="hidden lg:flex" />

      <HeaderButton label={t('common.language')} onClick={() => setLang(lang === 'en' ? 'ru' : 'en')} className="mono text-micro">
        {lang.toUpperCase()}
      </HeaderButton>

      <HeaderButton label={t('common.theme')} onClick={toggleTheme}>
        {theme === 'dark' ? <Sun /> : <Moon />}
      </HeaderButton>

      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-10 gap-2 rounded-surface border border-hairline-strong bg-surface px-2 pr-3 pointer-coarse:h-11"
            />
          }
        >
          <span className="flex size-5 items-center justify-center rounded-pill border border-hairline-strong text-micro font-medium text-foreground">
            {(user?.username ?? '?').charAt(0).toUpperCase()}
          </span>
          <span className="hidden max-w-28 truncate sm:inline" title={user?.username}>
            {user?.username}
          </span>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-56">
          <div className="lg:hidden">
            <StatusMenuRows />
            <DropdownMenuSeparator />
          </div>
          <DropdownMenuGroup>
            <DropdownMenuLabel className="flex items-center justify-between gap-2">
              <span className="truncate" title={user?.username}>
                {user?.username}
              </span>
              {user && <Badge>{t(ROLE_KEY[user.role] ?? 'common.role_viewer')}</Badge>}
            </DropdownMenuLabel>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => navigate('/settings?section=security')}>
            <ShieldCheck />
            {t('workspace.settings_security')}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => navigate('/settings?section=preferences')}>
            <SlidersHorizontal />
            {t('workspace.settings_preferences')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => void logout()}>
            <LogOut />
            {t('nav.logout')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </header>
  );
}
