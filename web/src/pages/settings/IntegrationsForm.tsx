import { ChartLine, Webhook } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { useSettings } from '@/api/settings';
import { CopyButton } from '@/components/common/CopyButton';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Skeleton } from '@/components/ui/skeleton';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { cn } from '@/lib/utils';

import { Arriving, FormColumns } from './formShell';

import type { ReactNode } from 'react';

const METRICS_SNIPPET = `scrape_configs:
  - job_name: tgwp-panel
    scheme: https
    scrape_interval: 60s
    authorization: { credentials_file: /etc/prometheus/tgwp-token }
    static_configs: [{ targets: ['<panel host>'] }]`;

function Code({ label, children }: { label: string; children: string }) {
  const { t } = useTranslation();
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between gap-2">
        <p className="text-label text-mute">{label}</p>
        <CopyButton value={children} label={`${t('common.copy')}: ${label}`} className="size-8" />
      </div>
      <pre className="mono overflow-x-auto rounded-control border border-hairline bg-background px-3 py-2.5 text-mono text-foreground">
        {children}
      </pre>
    </div>
  );
}

function State({ on, children }: { on: boolean; children: ReactNode }) {
  return (
    <span className="inline-flex items-center gap-2 text-label text-mute">
      <span className={cn('size-[7px] shrink-0 rounded-pill', on ? 'bg-ok' : 'bg-dim')} aria-hidden="true" />
      {children}
    </span>
  );
}

function DocsLine({ prefix, path }: { prefix: string; path: string }) {
  return (
    <p className="text-label text-mute">
      {prefix} <span className="mono text-mono text-foreground">{path}</span>
    </p>
  );
}

/** Metrics export and the alert webhook: both are set outside the panel, so this tab only shows how. */
export function IntegrationsForm() {
  const { t } = useTranslation();
  const settingsQuery = useSettings();

  if (settingsQuery.isLoading) {
    return (
      <FormColumns>
        <Skeleton className="h-72 w-full rounded-surface" />
        <Skeleton className="h-72 w-full rounded-surface" />
      </FormColumns>
    );
  }

  if (settingsQuery.isError) {
    return (
      <ErrorState
        message={settingsQuery.error instanceof ApiError ? settingsQuery.error.message : t('common.error_generic')}
        retryLabel={t('common.refresh')}
        onRetry={() => void settingsQuery.refetch()}
      />
    );
  }

  const tokenSet = !!settingsQuery.data?.metrics_token_set;
  const webhookOn = !!settingsQuery.data?.alert_webhook_configured;
  const webhookEnv = `ALERT_WEBHOOK_URL=https://alerts.example.com/tgproxy\nALERT_WEBHOOK_SECRET=${t('settings.integrations_webhook_secret_placeholder')}`;

  return (
    <FormColumns>
      <Arriving>
        <Panel>
          <PanelHeader
            icon={ChartLine}
            title={t('settings.integrations_prometheus_title')}
            actions={<HelpButton topic="settings.integrations" />}
          />
          <PanelBody className="space-y-4">
            <State on={tokenSet}>
              {tokenSet ? t('settings.integrations_token_set') : t('settings.integrations_token_missing')}
            </State>
            <p className="max-w-[72ch] text-body text-mute">{t('settings.integrations_prometheus_description')}</p>
            <Code label={t('settings.integrations_snippet')}>{METRICS_SNIPPET}</Code>
            <p className="max-w-[72ch] text-label text-mute">{t('settings.integrations_node_note')}</p>
            <p className="max-w-[72ch] text-label text-mute">{t('settings.integrations_grafana')}</p>
            <DocsLine prefix={t('settings.integrations_docs')} path={t('settings.integrations_docs_metrics')} />
          </PanelBody>
        </Panel>
      </Arriving>
      <Arriving index={1}>
        <Panel>
          <PanelHeader
            icon={Webhook}
            title={t('settings.integrations_webhook_title')}
            actions={<HelpButton topic="settings.integrations" />}
          />
          <PanelBody className="space-y-4">
            <State on={webhookOn}>
              {webhookOn ? t('settings.integrations_webhook_on') : t('settings.integrations_webhook_off')}
            </State>
            <p className="max-w-[72ch] text-body text-mute">{t('settings.integrations_webhook_description')}</p>
            <p className="max-w-[72ch] text-label text-mute">{t('settings.integrations_webhook_how')}</p>
            <Code label=".env">{webhookEnv}</Code>
            <p className="max-w-[72ch] text-label text-mute">{t('settings.integrations_webhook_rules')}</p>
            <DocsLine prefix={t('settings.integrations_webhook_docs')} path={t('settings.integrations_docs_webhook')} />
          </PanelBody>
        </Panel>
      </Arriving>
    </FormColumns>
  );
}
