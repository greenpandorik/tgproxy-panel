import { useTranslation } from 'react-i18next';
import { useAuth } from '@/auth/AuthProvider';
import { PageHeader } from '@/components/common/PageHeader';
import { SectionNav, useSection } from '@/components/common/SectionNav';
import { HelpButton, type HelpTopic } from '@/help';
import { AdminsForm } from './AdminsForm';
import { BackupsForm } from './BackupsForm';
import { BrandingProfilesList } from './BrandingProfilesList';
import { PreferencesForm } from './PreferencesForm';
import { PanelForm } from './PanelForm';
import { SecurityForm } from './SecurityForm';

const TAB_HELP: Record<string, HelpTopic> = {
  branding: 'settings.branding',
  preferences: 'settings.branding',
  security: 'settings.security',
  admins: 'settings.admins',
  panel: 'settings.panel',
  backups: 'settings.backups',
};

export function SettingsPage() {
  const { t } = useTranslation();
  const { isOwner } = useAuth();
  const allowed = ['panel', 'preferences', 'branding', 'security', ...(isOwner ? ['admins', 'backups'] : [])];
  const [section, setSection] = useSection(allowed, 'panel');
  const items = allowed.map((value) => ({
    value,
    label: t(`workspace.settings_${value}`),
    description: t(`workspace.settings_${value}_hint`),
  }));
  return (
    <>
      <PageHeader
        title={t('workspace.panel_settings')}
        description={t('workspace.panel_settings_hint')}
        actions={<HelpButton topic={TAB_HELP[section]} />}
      />
      <div className="flex min-w-0 flex-col items-start gap-6 lg:flex-row">
        <SectionNav label={t('workspace.panel_settings')} items={items} value={section} onChange={setSection} />
        <section className="min-w-0 w-full flex-1 space-y-5" aria-label={t(`workspace.settings_${section}`)}>
          <div className="space-y-1">
            <h2 className="text-title">{t(`workspace.settings_${section}`)}</h2>
            <p className="max-w-[72ch] text-body text-mute">{t(`workspace.settings_${section}_hint`)}</p>
          </div>
          {section === 'panel' && <PanelForm />}
          {section === 'preferences' && <PreferencesForm />}
          {section === 'branding' && <BrandingProfilesList />}
          {section === 'security' && <SecurityForm />}
          {section === 'admins' && isOwner && <AdminsForm />}
          {section === 'backups' && isOwner && <BackupsForm />}
        </section>
      </div>
    </>
  );
}
