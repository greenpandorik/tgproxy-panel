import { zodResolver } from '@hookform/resolvers/zod';
import { ExternalLink, FileText, Link2, ListChecks, Monitor, Server } from 'lucide-react';
import { useEffect, useState } from 'react';
import { Controller, useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { useNodes } from '@/api/nodes';
import { usePutSettings, useSettings } from '@/api/settings';
import { useAuth } from '@/auth/AuthProvider';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { cn } from '@/lib/utils';

import { Arriving, FormFooter } from './formShell';

import type { SubscriptionPlatform } from '@/api/types';

const schema = z
  .object({
    language: z.enum(['auto', 'ru', 'en']),
    platform: z.enum(['auto', 'android', 'ios', 'desktop']),
    title: z.string().max(80),
    intro: z.string().max(500),
    show_fake_tls: z.boolean(),
    show_web: z.boolean(),
    show_backup_domains: z.boolean(),
    show_guide: z.boolean(),
    show_status: z.boolean(),
    show_qr: z.boolean(),
    hidden_nodes: z.array(z.string()),
  })
  .refine((v) => v.show_fake_tls || v.show_web, { path: ['show_fake_tls'], message: 'links_required' });

type FormValues = z.infer<typeof schema>;

const DEFAULTS: FormValues = {
  language: 'ru',
  platform: 'android',
  title: '',
  intro: '',
  show_fake_tls: true,
  show_web: true,
  show_backup_domains: true,
  show_guide: true,
  show_status: true,
  show_qr: true,
  hidden_nodes: [],
};

type SwitchField = 'show_fake_tls' | 'show_web' | 'show_backup_domains' | 'show_guide' | 'show_status' | 'show_qr';

function SwitchRow({
  name,
  label,
  hint,
  control,
  disabled,
}: {
  name: SwitchField;
  label: string;
  hint?: string;
  control: ReturnType<typeof useForm<FormValues>>['control'];
  disabled: boolean;
}) {
  const id = `subpage-${name}`;
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0">
        <Label htmlFor={id}>{label}</Label>
        {hint && <p className="mt-0.5 text-label text-mute">{hint}</p>}
      </div>
      <Controller
        control={control}
        name={name}
        render={({ field }) => <Switch id={id} checked={field.value} onCheckedChange={field.onChange} disabled={disabled} />}
      />
    </div>
  );
}

export function SubscriptionPageForm() {
  const { t, i18n } = useTranslation();
  const { isOwner, isWriter } = useAuth();
  const settingsQuery = useSettings();
  const nodesQuery = useNodes();
  const putSettings = usePutSettings();
  const [src, setSrc] = useState('');
  const disabled = !isOwner;

  const {
    control,
    register,
    reset,
    handleSubmit,
    formState: { isDirty, isSubmitting },
  } = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: DEFAULTS });

  useEffect(() => {
    if (settingsQuery.data?.subscription_page) reset({ ...DEFAULTS, ...settingsQuery.data.subscription_page });
  }, [settingsQuery.data, reset]);

  const values = useWatch({ control }) as FormValues;
  const noLinks = !values.show_fake_tls && !values.show_web;
  const titleTooLong = (values.title ?? '').length > 80;
  const introTooLong = (values.intro ?? '').length > 500;
  const platform: SubscriptionPlatform = values.platform && values.platform !== 'auto' ? values.platform : 'android';
  const previewLanguage = values.language === 'auto' ? (i18n.language?.startsWith('en') ? 'en' : 'ru') : values.language;
  const payload = JSON.stringify({ values, platform, previewLanguage });

  useEffect(() => {
    if (!isWriter || !settingsQuery.data || (!values.show_fake_tls && !values.show_web)) return;
    const handle = window.setTimeout(() => {
      const bytes = new TextEncoder().encode(JSON.stringify(values));
      const encoded = btoa(Array.from(bytes, (b) => String.fromCharCode(b)).join(''));
      const query = new URLSearchParams({ platform, language: previewLanguage, settings: encoded });
      setSrc(`/api/v1/settings/subscription-page/preview?${query.toString()}`);
    }, 350);
    return () => window.clearTimeout(handle);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [payload, isWriter, settingsQuery.data]);

  const onSubmit = async (v: FormValues) => {
    try {
      await putSettings.mutateAsync({ subscription_page: { ...v, title: v.title.trim(), intro: v.intro.trim() } });
      toast.add({ description: t('settings.panel_save_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  if (settingsQuery.isLoading) {
    return <Skeleton className="h-96 w-full" />;
  }
  if (settingsQuery.isError) {
    return (
      <ErrorState
        message={settingsQuery.error.message}
        onRetry={() => void settingsQuery.refetch()}
        retryLabel={t('common.refresh')}
      />
    );
  }

  const nodes = nodesQuery.data?.items ?? [];
  const noServers = nodes.length > 0 && nodes.every((n) => (values.hidden_nodes ?? []).includes(n.id));

  return (
    <form
      className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(0,440px)]"
      onSubmit={(e) => void handleSubmit(onSubmit)(e)}
      noValidate
    >
      <div className="flex min-w-0 flex-col gap-4">
        <Arriving index={0}>
          <Panel>
            <PanelHeader
              icon={FileText}
              title={t('settings.subpage_content')}
              actions={<HelpButton topic="settings.subscription" />}
            />
            <PanelBody className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="subpage-language">{t('settings.subpage_language')}</Label>
                <select id="subpage-language" className="ops-select w-full max-w-sm" disabled={disabled} {...register('language')}>
                  <option value="ru">{t('settings.panel_telegram_language_ru')}</option>
                  <option value="en">{t('settings.panel_telegram_language_en')}</option>
                  <option value="auto">{t('settings.subpage_language_auto')}</option>
                </select>
              </div>
              <div className="space-y-2">
                <Label htmlFor="subpage-platform">{t('settings.subpage_platform')}</Label>
                <select
                  id="subpage-platform"
                  className="ops-select w-full max-w-sm"
                  disabled={disabled}
                  aria-describedby="subpage-platform-hint"
                  {...register('platform')}
                >
                  <option value="android">{t('subpage.platform_android')}</option>
                  <option value="ios">{t('subpage.platform_ios')}</option>
                  <option value="desktop">{t('subpage.platform_desktop')}</option>
                  <option value="auto">{t('settings.subpage_platform_auto')}</option>
                </select>
                <p id="subpage-platform-hint" className="text-label text-mute">
                  {t('settings.subpage_platform_hint')}
                </p>
              </div>
              <div className="space-y-2">
                <Label htmlFor="subpage-title">{t('settings.subpage_title')}</Label>
                <Input
                  id="subpage-title"
                  disabled={disabled}
                  aria-invalid={titleTooLong || undefined}
                  aria-describedby="subpage-title-hint"
                  {...register('title')}
                />
                <p id="subpage-title-hint" className="flex justify-between gap-3 text-label text-mute">
                  <span className={titleTooLong ? 'text-err' : undefined}>
                    {titleTooLong ? t('settings.subpage_title_too_long') : t('settings.subpage_title_hint')}
                  </span>
                  <span className={titleTooLong ? 'shrink-0 tabular-nums text-err' : 'shrink-0 tabular-nums'}>
                    {t('settings.subpage_title_count', { count: (values.title ?? '').length })}
                  </span>
                </p>
              </div>
              <div className="space-y-2">
                <Label htmlFor="subpage-intro">{t('settings.subpage_intro')}</Label>
                <Textarea
                  id="subpage-intro"
                  rows={3}
                  disabled={disabled}
                  aria-invalid={introTooLong || undefined}
                  aria-describedby="subpage-intro-hint"
                  {...register('intro')}
                />
                <p id="subpage-intro-hint" className={introTooLong ? 'text-label text-err' : 'text-label text-mute'}>
                  {introTooLong ? t('settings.subpage_intro_too_long') : t('settings.subpage_intro_hint')}
                </p>
              </div>
            </PanelBody>
          </Panel>
        </Arriving>

        <Arriving index={1}>
          <Panel>
            <PanelHeader icon={Link2} title={t('settings.subpage_links')} />
            <PanelBody className="space-y-4">
              <SwitchRow
                name="show_fake_tls"
                label={t('settings.subpage_show_fake_tls')}
                hint={t('settings.subpage_show_fake_tls_hint')}
                control={control}
                disabled={disabled}
              />
              <SwitchRow
                name="show_web"
                label={t('settings.subpage_show_web')}
                hint={t('settings.subpage_show_web_hint')}
                control={control}
                disabled={disabled}
              />
              <SwitchRow
                name="show_backup_domains"
                label={t('settings.subpage_show_backup')}
                hint={t('settings.subpage_show_backup_hint')}
                control={control}
                disabled={disabled || !values.show_fake_tls}
              />
              {noLinks ? (
                <p role="alert" className="text-label text-err">
                  {t('settings.subpage_links_required')}
                </p>
              ) : (
                !values.show_fake_tls && (
                  <p role="status" className="text-label text-warn">
                    {t('settings.subpage_no_tls_warning')}
                  </p>
                )
              )}
            </PanelBody>
          </Panel>
        </Arriving>

        <Arriving index={2}>
          <Panel>
            <PanelHeader icon={ListChecks} title={t('settings.subpage_blocks')} />
            <PanelBody className="space-y-4">
              <SwitchRow name="show_guide" label={t('settings.subpage_show_guide')} control={control} disabled={disabled} />
              <SwitchRow name="show_status" label={t('settings.subpage_show_status')} control={control} disabled={disabled} />
              <SwitchRow name="show_qr" label={t('settings.subpage_show_qr')} control={control} disabled={disabled} />
            </PanelBody>
          </Panel>
        </Arriving>

        <Arriving index={3}>
          <Panel>
            <PanelHeader icon={Server} title={t('settings.subpage_servers')} />
            <PanelBody className="space-y-3">
              <p className="text-label text-mute">{t('settings.subpage_servers_hint')}</p>
              {nodes.length === 0 && <p className="text-body text-mute">{t('settings.subpage_no_servers')}</p>}
              <Controller
                control={control}
                name="hidden_nodes"
                render={({ field }) => (
                  <ul className="space-y-2">
                    {nodes.map((n) => {
                      const shown = !field.value.includes(n.id);
                      return (
                        <li key={n.id}>
                          <label className="flex items-center gap-3">
                            <Checkbox
                              checked={shown}
                              disabled={disabled}
                              onCheckedChange={(on) =>
                                field.onChange(on ? field.value.filter((id) => id !== n.id) : [...field.value, n.id])
                              }
                            />
                            <span className="text-body">{n.name}</span>
                            <span className="mono truncate text-label text-mute">{n.hostname}</span>
                          </label>
                        </li>
                      );
                    })}
                  </ul>
                )}
              />
              {noServers && (
                <p role="alert" className="text-label text-err">
                  {t('settings.subpage_servers_required')}
                </p>
              )}
            </PanelBody>
          </Panel>
        </Arriving>

        <FormFooter note={!isOwner ? t('settings.panel_owner_only_note') : undefined}>
          {isOwner && (
            <Button type="submit" disabled={isSubmitting || !isDirty || noLinks || noServers || titleTooLong || introTooLong}>
              {t('common.save')}
            </Button>
          )}
        </FormFooter>
      </div>

      {isWriter && (
        <div className="min-w-0 xl:sticky xl:top-2 xl:self-start">
          <Panel>
            <PanelHeader
              icon={Monitor}
              title={t('settings.subpage_preview')}
              actions={
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={!src}
                  onClick={() => window.open(src, '_blank', 'noopener')}
                >
                  <ExternalLink />
                  {t('settings.subpage_preview_open')}
                </Button>
              }
            />
            <PanelBody className="space-y-3">
              {noLinks && (
                <p role="status" className="text-label text-warn">
                  {t('settings.subpage_preview_stale')}
                </p>
              )}
              <div
                className={cn(
                  'overflow-hidden rounded-surface border border-hairline-strong',
                  noLinks && 'opacity-50',
                )}
              >
                {src ? (
                  <iframe
                    title={t('settings.subpage_preview')}
                    sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox"
                    src={src}
                    className="block h-[680px] w-full border-0 bg-background"
                  />
                ) : (
                  <Skeleton className="h-[680px] w-full rounded-none" />
                )}
              </div>
              <p className="text-label text-mute">{t('settings.subpage_preview_hint')}</p>
            </PanelBody>
          </Panel>
        </div>
      )}
    </form>
  );
}
