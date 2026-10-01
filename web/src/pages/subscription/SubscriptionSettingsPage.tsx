import { useTranslation } from 'react-i18next';

import { PageHeader } from '@/components/common/PageHeader';
import { SectionTabs, useSection } from '@/components/common/SectionNav';
import { HelpButton } from '@/help';

import { SubscriptionPageForm } from './SubscriptionPageForm';
import { SubscriptionServiceSection } from './SubscriptionServicePage';

export function SubscriptionSettingsPage() {
  const { t } = useTranslation();
  const [section, setSection] = useSection(['look', 'service'], 'look');
  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title={t('nav.subscription_page')}
        description={t('workspace.settings_subscription_hint')}
        actions={<HelpButton topic={section === 'service' ? 'subscription.service' : 'settings.subscription'} />}
      />
      <SectionTabs
        label={t('workspace.subscription_navigation')}
        value={section}
        onChange={setSection}
        items={[
          { value: 'look', label: t('workspace.subscription_tab_look') },
          { value: 'service', label: t('workspace.subscription_tab_service') },
        ]}
      />
      {section === 'service' ? <SubscriptionServiceSection /> : <SubscriptionPageForm />}
    </div>
  );
}
