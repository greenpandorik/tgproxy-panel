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
import type { Lang } from '@/i18n';
import { setLang } from '@/i18n';
import { useTheme } from '@/theme/ThemeProvider';

import { navItemForPath } from './nav';
import { StatusMenuRows } from './StatusChips';

const ROLE_KEY: Record<string, string> = {
  owner: 'common.role_owner',
  admin: 'common.role_admin',
  viewer: 'common.role_viewer',
};

/** `ru | en`, a hairline pair rather than a dropdown - there are only two. */
function LanguageSwitch() {
  const { t, i18n } = useTranslation();
  const current = (i18n.language?.startsWith('en') ? 'en' : 'ru') as Lang;

  return (
    <div
      className="mono flex h-7 items-stretch overflow-hidden rounded-control border border-hairline-strong text-micro"
      role="group"
      aria-label={t('common.language')}
    >
      {(['ru', 'en'] as const).map((lang) => (
        <button
          key={lang}
          type="button"
          onClick={() => setLang(lang)}
          aria-pressed={current === lang}
          className={
            'flex h-full items-center px-2 transition-[background-color,color,scale] duration-fast ease-out active:scale-[0.985] ' +
            (current === lang ? 'bg-elevated text-foreground' : 'text-mute hover:text-foreground')
          }
        >
          {lang}
        </button>
      ))}
    </div>
  );
}

interface TopbarProps {
  onOpenMenu: () => void;
  onOpenCommand: () => void;
}

export function Topbar({ onOpenMenu, onOpenCommand }: TopbarProps) {
  const { t } = useTranslation();
  const { theme, toggleTheme } = useTheme();
  const { user, logout } = useAuth();
  const location = useLocation();
  const section = navItemForPath(location.pathname);
  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

  return (
    <header className="flex min-h-16 shrink-0 items-center gap-3 border-b border-hairline bg-card px-3 sm:px-5">
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

      {/*
        Not an input: it is a button that opens the palette, so there is only
        one search box in the product and one place typing goes.
      */}
      <button
        type="button"
        onClick={onOpenCommand}
        className="hidden h-9 w-56 items-center gap-2 rounded-control border border-hairline-strong px-2.5 text-label text-mute transition-[background-color,color,scale] duration-fast ease-out hover:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none active:scale-[0.985] md:flex xl:w-64"
      >
        <Search className="size-3.5 shrink-0" aria-hidden="true" />
        <span className="truncate">{t('shell.command_placeholder')}</span>
        <Badge render={<kbd />} className="ml-auto">
          {isMac ? '⌘K' : 'Ctrl K'}
        </Badge>
      </button>

      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="md:hidden"
        onClick={onOpenCommand}
        aria-label={t('shell.command_placeholder')}
      >
        <Search />
      </Button>

      <LanguageSwitch />

      <Button type="button" variant="ghost" size="icon-sm" onClick={toggleTheme} aria-label={t('common.theme')}>
        {theme === 'dark' ? <Sun /> : <Moon />}
      </Button>

      <DropdownMenu>
        <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="sm" className="gap-2 px-1.5" />}>
          <span className="flex size-5 items-center justify-center rounded-pill border border-hairline-strong text-micro font-medium text-foreground">
            {(user?.username ?? '?').charAt(0).toUpperCase()}
          </span>
          <span className="hidden max-w-28 truncate sm:inline">{user?.username}</span>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-56">
          {/* Below md the topbar has no room for the status chips, so the
              same facts open the menu as plain rows. */}
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
