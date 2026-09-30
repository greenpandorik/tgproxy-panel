import { useTranslation } from 'react-i18next';

import { PageHeader } from '@/components/common/PageHeader';
import { HelpButton } from '@/help';

import { SubscriptionPageForm } from './SubscriptionPageForm';

export function SubscriptionSettingsPage() {
  const { t } = useTranslation();
  return (
    <>
      <PageHeader
        title={t('workspace.settings_subscription')}
        description={t('workspace.settings_subscription_hint')}
        actions={<HelpButton topic="settings.subscription" />}
      />
      <SubscriptionPageForm />
    </>
  );
}
