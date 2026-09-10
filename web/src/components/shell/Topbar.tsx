import { LogOut, Menu, Moon, Search, Sun } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useLocation } from 'react-router-dom';

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

import { navItemForPath } from './nav';
import { StatusChips, StatusMenuRows } from './StatusChips';

const ROLE_KEY: Record<string, string> = {
  owner: 'common.role_owner',
  admin: 'common.role_admin',
  viewer: 'common.role_viewer',
};

/** One square in the header's control row. The chips next to it carry the same height and radius. */
const SQUARE = 'size-9 rounded-surface border border-hairline-strong text-mute hover:text-foreground';

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
          <Button type="button" variant="ghost" size="icon-sm" onClick={onClick} aria-label={label} className={cn(SQUARE, className)} />
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
  const { theme, toggleTheme } = useTheme();
  const { user, logout } = useAuth();
  const location = useLocation();
  const section = navItemForPath(location.pathname);
  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
  const lang = (i18n.language?.startsWith('en') ? 'en' : 'ru') as Lang;

  return (
    <header className="flex min-h-16 shrink-0 items-center gap-2 border-b border-hairline bg-card px-3 sm:px-5">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="lg:hidden"
        onClick={onOpenMenu}
        aria-label={t('shell.open_menu')}
      >
        <Menu />
      </Button>

      <p className="min-w-0 flex-1 truncate text-label text-mute">
        {t('shell.breadcrumb_root')}
        {section && (
          <>
            <span className="px-1.5">/</span>
            <span className="text-foreground">{t(section.labelKey)}</span>
          </>
        )}
      </p>

      {/* The palette is the only search in the product, so this opens it rather than taking text. */}
      <HeaderButton label={`${t('shell.command_placeholder')} (${isMac ? '⌘K' : 'Ctrl K'})`} onClick={onOpenCommand}>
        <Search />
      </HeaderButton>

      {/* Below lg the row would not fit; the same facts are rows in the menu. */}
      <StatusChips className="hidden lg:flex" />

      <HeaderButton
        label={t('common.language')}
        onClick={() => setLang(lang === 'en' ? 'ru' : 'en')}
        className="mono text-micro"
      >
        {lang.toUpperCase()}
      </HeaderButton>

      <HeaderButton label={t('common.theme')} onClick={toggleTheme}>
        {theme === 'dark' ? <Sun /> : <Moon />}
      </HeaderButton>

      <DropdownMenu>
        <DropdownMenuTrigger
          render={<Button type="button" variant="ghost" size="sm" className="h-9 gap-2 rounded-surface border border-hairline-strong px-1.5 pr-2.5" />}
        >
          <span className="flex size-5 items-center justify-center rounded-pill border border-hairline-strong text-micro font-medium text-foreground">
            {(user?.username ?? '?').charAt(0).toUpperCase()}
          </span>
          <span className="hidden max-w-28 truncate sm:inline">{user?.username}</span>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-56">
          {/* Below lg the header has no room for the chips, so the same facts open the menu as rows. */}
          <StatusMenuRows />
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel className="flex items-center justify-between gap-2">
              <span className="truncate">{user?.username}</span>
              {user && <Badge>{t(ROLE_KEY[user.role] ?? 'common.role_viewer')}</Badge>}
            </DropdownMenuLabel>
          </DropdownMenuGroup>
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
