import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useAuth } from '@/auth/AuthProvider';
import { PageHeader } from '@/components/common/PageHeader';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { HelpButton, type HelpTopic } from '@/help';

import { AdminsForm } from './AdminsForm';
import { BackupsForm } from './BackupsForm';
import { BrandingProfilesList } from './BrandingProfilesList';
import { PreferencesForm } from './PreferencesForm';
import { PanelForm } from './PanelForm';
import { SecurityForm } from './SecurityForm';

/** The help topic of each tab: the header's `?` follows the selected tab. */
const TAB_HELP: Record<string, HelpTopic> = {
  branding: 'settings.branding',
  security: 'settings.security',
  admins: 'settings.admins',
  panel: 'settings.panel',
  backups: 'settings.backups',
};

export function SettingsPage() {
  const { t } = useTranslation();
  const { isOwner } = useAuth();
  const [tab, setTab] = useState('branding');

  return (
    <>
      <PageHeader title={t('settings.title')} actions={<HelpButton topic={TAB_HELP[tab] ?? 'settings.branding'} />} />

      <Tabs value={tab} onValueChange={(v) => setTab(String(v))}>
        {/* Five labels do not fit at 390px; the row scrolls rather than clipping the last tab out of reach. */}
        <TabsList className="w-full justify-start overflow-x-auto">
          <TabsTrigger value="branding">{t('settings.tab_branding')}</TabsTrigger>
          <TabsTrigger value="security">{t('settings.tab_security')}</TabsTrigger>
          {isOwner && <TabsTrigger value="admins">{t('settings.tab_admins')}</TabsTrigger>}
          <TabsTrigger value="panel">{t('settings.tab_panel')}</TabsTrigger>
          {/* Owner only, and not merely hidden: a dump is the whole database, so
              every /backups route rejects anyone else. */}
          {isOwner && <TabsTrigger value="backups">{t('settings.tab_backups')}</TabsTrigger>}
        </TabsList>

        <TabsContent value="branding" className="pt-4">
          <div className="space-y-6">
            <PreferencesForm />
            <div className="space-y-1">
              <h2 className="text-title">{t('preferences.project_title')}</h2>
              <p className="text-body text-mute">{t('preferences.project_description')}</p>
            </div>
            <BrandingProfilesList />
          </div>
        </TabsContent>
        <TabsContent value="security" className="pt-4">
          <SecurityForm />
        </TabsContent>
        {isOwner && (
          <TabsContent value="admins" className="pt-4">
            <AdminsForm />
          </TabsContent>
        )}
        <TabsContent value="panel" className="pt-4">
          <PanelForm />
        </TabsContent>
        {isOwner && (
          <TabsContent value="backups" className="pt-4">
            <BackupsForm />
          </TabsContent>
        )}
      </Tabs>
    </>
  );
}
