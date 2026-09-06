import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Outlet } from 'react-router-dom';

import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet';

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
    <div className="flex h-screen overflow-hidden bg-background">
      <aside
        className={
          'hidden shrink-0 border-r border-hairline transition-[width] duration-150 lg:block ' +
          (collapsed ? 'w-[60px]' : 'w-56')
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
        <main className="flex-1 overflow-y-auto p-4 sm:p-5">
          <div className="flex w-full flex-col gap-4">
            <Outlet />
          </div>
        </main>
      </div>

      <CommandPalette open={commandOpen} onOpenChange={setCommandOpen} />
    </div>
  );
}
