import type { RouteObject } from 'react-router-dom';

import { RequireAuth } from '@/auth/RequireAuth';
import { AppShell } from '@/components/shell/AppShell';
import { DashboardPage } from '@/pages/DashboardPage';
import { LoginPage } from '@/pages/LoginPage';
import { AuditPage } from '@/pages/audit/AuditPage';
import { KeysPage } from '@/pages/keys/KeysPage';
import { MonitoringPage } from '@/pages/monitoring/MonitoringPage';
import { NodeDetailPage } from '@/pages/nodes/NodeDetailPage';
import { NodesPage } from '@/pages/nodes/NodesPage';
import { SettingsPage } from '@/pages/settings/SettingsPage';
import { SiteTemplatesPage } from '@/pages/sites/SiteTemplatesPage';
import { TemplateEditorPage } from '@/pages/sites/TemplateEditorPage';

export const routes: RouteObject[] = [
  { path: '/login', element: <LoginPage /> },
  {
    path: '/',
    element: (
      <RequireAuth>
        <AppShell />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <DashboardPage /> },
      { path: 'nodes', element: <NodesPage /> },
      { path: 'nodes/:id', element: <NodeDetailPage /> },
      { path: 'keys', element: <KeysPage /> },
      { path: 'sites', element: <SiteTemplatesPage /> },
      { path: 'sites/new', element: <TemplateEditorPage /> },
      { path: 'sites/:id', element: <TemplateEditorPage /> },
      { path: 'monitoring', element: <MonitoringPage /> },
      { path: 'audit', element: <AuditPage /> },
      { path: 'settings', element: <SettingsPage /> },
    ],
  },
];
