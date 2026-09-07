import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Outlet } from 'react-router-dom';

import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet';
import { HelpProvider } from '@/help';

import { CommandPalette } from './CommandPalette';
import { Sidebar } from './Sidebar';
import { Topbar } from './Topbar';

const SIDEBAR_STORAGE_KEY = 'sidebar-collapsed';

function storedCollapsed(): boolean {
  return window.localStorage.getItem(SIDEBAR_STORAGE_KEY) === '1';
}

/**
 * Shell for every authenticated route: a 224px grouped rail on the left
 * (collapsible to an icon rail, persisted) that becomes a Sheet drawer below
 * 1024px, a 48px topbar, the routed page, and the ⌘K palette mounted once.
 */
export function AppShell() {
  const { t } = useTranslation();
  const [collapsed, setCollapsed] = useState(storedCollapsed);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [commandOpen, setCommandOpen] = useState(false);

  useEffect(() => {
    window.localStorage.setItem(SIDEBAR_STORAGE_KEY, collapsed ? '1' : '0');
  }, [collapsed]);

  return (
    <HelpProvider>
      <div className="flex h-screen overflow-hidden bg-background">
        <aside
          className={
            'hidden shrink-0 border-r border-hairline transition-[width] duration-base ease-out lg:block ' +
            (collapsed ? 'w-15' : 'w-56')
          }
        >
          <Sidebar collapsed={collapsed} showToggle onToggle={() => setCollapsed((c) => !c)} />
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
          <main className="flex-1 overflow-y-auto p-4 sm:p-6">
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
