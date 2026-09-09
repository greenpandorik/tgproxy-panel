import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Outlet, useLocation } from 'react-router-dom';

import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet';
import { HelpProvider } from '@/help';
import { useTheme } from '@/theme/ThemeProvider';

import { CommandPalette } from './CommandPalette';
import { Sidebar } from './Sidebar';
import { Topbar } from './Topbar';

export function AppShell() {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  const mainRef = useRef<HTMLElement>(null);

  useEffect(() => {
    if (mainRef.current) mainRef.current.scrollTop = 0;
  }, [pathname]);
  const { collapsed, setCollapsed } = useTheme();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [commandOpen, setCommandOpen] = useState(false);

  useEffect(() => {
    const previous = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = previous;
    };
  }, []);

  return (
    <HelpProvider>
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-control focus:bg-card focus:p-3"
      >
        {t('shell.skip_content')}
      </a>
      <div className="flex h-dvh overflow-hidden bg-background">
        <aside
          className={
            'hidden shrink-0 border-r border-hairline transition-[width] duration-base ease-out lg:block ' +
            (collapsed ? 'w-15' : 'w-64')
          }
        >
          <Sidebar collapsed={collapsed} showToggle onToggle={() => setCollapsed(!collapsed)} />
        </aside>

        <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
          <SheetContent side="left" className="w-64 p-0">
            <SheetTitle className="sr-only">{t('shell.menu')}</SheetTitle>
            <Sidebar onNavigate={() => setMobileOpen(false)} />
          </SheetContent>
        </Sheet>

        <div className="flex min-w-0 flex-1 flex-col">
          <Topbar onOpenMenu={() => setMobileOpen(true)} onOpenCommand={() => setCommandOpen(true)} />
          {/*
            The page's own rhythm, set once here: 16/24px of ground around the
            content and 24px between the blocks a page stacks. Panels inside a
            block still sit 16px apart, so a row of tiles reads as one thing and
            the sections read as several.
          */}
          <main ref={mainRef} id="main-content" tabIndex={-1} className="flex-1 overflow-y-auto p-4 sm:p-6">
            <div className="flex w-full flex-col gap-6">
              <Outlet />
            </div>
          </main>
        </div>

        <CommandPalette open={commandOpen} onOpenChange={setCommandOpen} />
      </div>
    </HelpProvider>
  );
}
