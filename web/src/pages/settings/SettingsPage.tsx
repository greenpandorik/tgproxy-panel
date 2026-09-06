import { useTranslation } from 'react-i18next';

import { useAuth } from '@/auth/AuthProvider';
import { PageHeader } from '@/components/common/PageHeader';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

import { AdminsForm } from './AdminsForm';
import { BackupsForm } from './BackupsForm';
import { BrandingProfilesList } from './BrandingProfilesList';
import { PanelForm } from './PanelForm';
import { SecurityForm } from './SecurityForm';

export function SettingsPage() {
  const { t } = useTranslation();
  const { isOwner } = useAuth();

  return (
    <>
      <PageHeader title={t('settings.title')} />

      <Tabs defaultValue="branding">
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
          <BrandingProfilesList />
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
