import { ExternalLink, Globe, KeyRound, Server } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import {
  useIssueServiceToken,
  usePutSubscriptionService,
  useRevokeServiceToken,
  useSubscriptionService,
} from '@/api/subscriptionService';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { CopyButton } from '@/components/common/CopyButton';
import { ErrorState } from '@/components/common/ErrorState';
import { PageHeader } from '@/components/common/PageHeader';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { SegmentedControl } from '@/components/common/SegmentedControl';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { formatDateTime, formatRelativeTime } from '@/lib/format';

import type { ServiceToken, SubscriptionService } from '@/api/subscriptionService';

type Where = 'panel' | 'own';

function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return '';
  }
}

function CommandBox({ value, label }: { value: string; label: string }) {
  return (
    <div className="mt-2 flex items-start gap-2">
      <pre className="mono min-w-0 flex-1 overflow-x-auto rounded-control border border-hairline bg-background px-3 py-2 text-mono whitespace-pre-wrap break-all">
        {value}
      </pre>
      <CopyButton
        value={value}
        label={label}
        className="size-(--control-height) shrink-0 border-hairline-strong bg-surface text-foreground hover:bg-elevated"
      />
    </div>
  );
}

export function SubscriptionServicePage() {
  const { t } = useTranslation();
  const query = useSubscriptionService();
  const svc = query.data;
  if (query.isLoading) return <Skeleton className="h-96 w-full" />;
  if (query.isError || !svc) {
    return <ErrorState message={query.error?.message ?? ''} onRetry={() => void query.refetch()} retryLabel={t('common.refresh')} />;
  }
  return <ServiceForm key={`${svc.public_url}|${svc.hide_on_panel}`} svc={svc} />;
}

function ServiceForm({ svc }: { svc: SubscriptionService }) {
  const { t, i18n } = useTranslation();
  const { isOwner } = useAuth();
  const put = usePutSubscriptionService();
  const issue = useIssueServiceToken();
  const revoke = useRevokeServiceToken();

  const [where, setWhere] = useState<Where>(svc.public_url ? 'own' : 'panel');
  const [domain, setDomain] = useState(hostOf(svc.public_url));
  const [hide, setHide] = useState(svc.hide_on_panel);
  const [issued, setIssued] = useState<ServiceToken | null>(null);
  const [confirmRevoke, setConfirmRevoke] = useState(false);
  const [confirmReissue, setConfirmReissue] = useState(false);
  const [error, setError] = useState('');

  const savedDomain = hostOf(svc.public_url);
  const wantDomain = where === 'own' ? domain.trim().toLowerCase().replace(/^https?:\/\//, '').replace(/\/+$/, '') : '';
  const dirty = wantDomain !== savedDomain || (where === 'own' && hide !== svc.hide_on_panel);
  const panelHost = hostOf(svc.panel_url);
  const shownDomain = savedDomain || wantDomain || 'sub.example.com';

  const save = async () => {
    setError('');
    try {
      await put.mutateAsync({ public_url: wantDomain, hide_on_panel: where === 'own' && hide });
      toast.add({ description: t('service.saved'), type: 'success' });
    } catch (err) {
      if (err instanceof ApiError && err.fields.public_url) {
        setError(err.fields.public_url.includes('differ') ? t('service.domain_same_as_panel') : t('service.domain_invalid'));
        return;
      }
      setError(err instanceof ApiError ? err.message : t('common.error_generic'));
    }
  };

  const issueToken = async () => {
    try {
      setIssued(await issue.mutateAsync());
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <>
      <PageHeader
        title={t('service.title')}
        description={t('service.hint')}
        actions={<HelpButton topic="subscription.service" />}
      />

      <div className="grid gap-5 xl:grid-cols-2">
        <Panel>
          <PanelHeader icon={Globe} title={t('service.where_title')} />
          <PanelBody className="space-y-4">
            <SegmentedControl
              label={t('service.where_title')}
              value={where}
              onChange={(v) => setWhere(v as Where)}
              options={[
                { value: 'panel', label: t('service.where_panel') },
                { value: 'own', label: t('service.where_own') },
              ]}
            />
            {where === 'panel' ? (
              <p className="text-label text-mute">{t('service.where_panel_hint', { host: panelHost })}</p>
            ) : (
              <>
                <div className="space-y-2">
                  <Label htmlFor="service-domain">{t('service.domain')}</Label>
                  <Input
                    id="service-domain"
                    className="mono max-w-md text-mono"
                    placeholder="sub.example.com"
                    value={domain}
                    disabled={!isOwner}
                    onChange={(e) => setDomain(e.target.value)}
                    aria-invalid={!!error}
                  />
                  {error ? (
                    <p className="text-label text-destructive">{error}</p>
                  ) : (
                    <p className="text-label text-mute">{t('service.domain_hint')}</p>
                  )}
                </div>
                <label className="flex items-start gap-3">
                  <Checkbox checked={hide} disabled={!isOwner} onCheckedChange={(v) => setHide(!!v)} className="mt-0.5" />
                  <span className="space-y-0.5">
                    <span className="block text-body">{t('service.hide_on_panel')}</span>
                    <span className="block text-label text-mute">
                      {hide ? t('service.hide_on_panel_on', { host: panelHost }) : t('service.hide_on_panel_off', { host: panelHost })}
                    </span>
                  </span>
                </label>
              </>
            )}
            {isOwner && (
              <div className="flex flex-wrap items-center gap-2">
                <Button type="button" disabled={!dirty || put.isPending || (where === 'own' && !wantDomain)} onClick={() => void save()}>
                  {t('common.save')}
                </Button>
                {savedDomain && (
                  <Button
                    type="button"
                    variant="outline"
                    nativeButton={false}
                    render={<a href={`${svc.public_url}/healthz`} target="_blank" rel="noopener noreferrer" />}
                  >
                    <ExternalLink />
                    {t('service.check')}
                  </Button>
                )}
              </div>
            )}
            {!isOwner && <p className="text-label text-mute">{t('settings.panel_owner_only_note')}</p>}
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader icon={Server} title={t('service.same_title')} />
          <PanelBody className="space-y-3">
            <p className="text-label text-mute">{t('service.same_hint', { host: panelHost })}</p>
            <ol className="list-decimal space-y-2 pl-5 text-body">
              <li>{t('service.same_step_dns', { domain: shownDomain })}</li>
              <li>
                {t('service.same_step_command')}
                <CommandBox value={`sudo /opt/tgproxy-panel/install.sh --update --sub-domain ${shownDomain}`} label={t('service.copy_command')} />
              </li>
              <li>{t('service.same_step_save')}</li>
            </ol>
          </PanelBody>
        </Panel>

        <Panel className="xl:col-span-2">
          <PanelHeader
            icon={KeyRound}
            title={t('service.other_title')}
            meta={
              svc.token_set ? (
                <span className={svc.online ? 'text-label text-ok' : 'text-label text-warn'}>
                  {svc.online ? t('service.online') : t('service.offline')}
                </span>
              ) : undefined
            }
          />
          <PanelBody className="space-y-4">
            <p className="text-label text-mute">{t('service.other_hint')}</p>
            {svc.token_set && (
              <dl className="grid gap-x-6 gap-y-2 text-body sm:grid-cols-3">
                <div>
                  <dt className="text-label text-mute">{t('service.last_seen')}</dt>
                  <dd>
                    {svc.status.last_seen_at
                      ? `${formatRelativeTime(svc.status.last_seen_at, i18n.language)} · ${formatDateTime(svc.status.last_seen_at, i18n.language)}`
                      : t('service.never')}
                  </dd>
                </div>
                <div>
                  <dt className="text-label text-mute">{t('service.service_version')}</dt>
                  <dd>
                    <span className="mono text-mono">{svc.status.version || '—'}</span>
                    {svc.status.version && svc.status.version !== svc.version && svc.status.version !== 'installer' && (
                      <span className="ml-2 text-label text-warn">{t('service.version_differs', { version: svc.version })}</span>
                    )}
                  </dd>
                </div>
                <div>
                  <dt className="text-label text-mute">{t('service.address')}</dt>
                  <dd className="mono text-mono">{svc.status.address || '—'}</dd>
                </div>
              </dl>
            )}
            {issued ? (
              <div className="space-y-2 rounded-control border border-hairline-strong p-3">
                <p className="text-label text-warn">{t('service.command_once')}</p>
                <ol className="list-decimal space-y-2 pl-5 text-body">
                  <li>{t('service.other_step_dns', { domain: shownDomain })}</li>
                  <li>
                    {t('service.other_step_command')}
                    <CommandBox value={issued.command} label={t('service.copy_command')} />
                  </li>
                  <li>{t('service.other_step_save')}</li>
                </ol>
              </div>
            ) : null}
            {isOwner && (
              <div className="flex flex-wrap gap-2">
                {!svc.token_set ? (
                  <Button type="button" onClick={() => void issueToken()} disabled={issue.isPending}>
                    {t('service.get_command')}
                  </Button>
                ) : (
                  <>
                    <Button type="button" variant="outline" onClick={() => setConfirmReissue(true)}>
                      {t('service.reissue')}
                    </Button>
                    <Button type="button" variant="destructive" onClick={() => setConfirmRevoke(true)}>
                      {t('service.revoke')}
                    </Button>
                  </>
                )}
              </div>
            )}
          </PanelBody>
        </Panel>
      </div>

      <ConfirmDialog
        open={confirmReissue}
        onOpenChange={setConfirmReissue}
        title={t('service.reissue_title')}
        description={t('service.reissue_description')}
        confirmLabel={t('service.reissue')}
        onConfirm={issueToken}
      />
      <ConfirmDialog
        open={confirmRevoke}
        onOpenChange={setConfirmRevoke}
        title={t('service.revoke_title')}
        description={t('service.revoke_description')}
        destructive
        confirmLabel={t('service.revoke')}
        onConfirm={async () => {
          try {
            await revoke.mutateAsync();
            setIssued(null);
            toast.add({ description: t('service.revoked'), type: 'success' });
          } catch (err) {
            toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
          }
        }}
      />
    </>
  );
}
