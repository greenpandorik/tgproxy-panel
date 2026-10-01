import { zodResolver } from '@hookform/resolvers/zod';
import { ExternalLink, FileText, LifeBuoy, Link2, ListChecks, Monitor, RotateCcw, Server } from 'lucide-react';
import { useEffect, useState } from 'react';
import { Controller, useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { useBranding } from '@/api/branding';
import { useNodes } from '@/api/nodes';
import { usePutSettings, useSettings } from '@/api/settings';
import { useAuth } from '@/auth/AuthProvider';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { SegmentedControl } from '@/components/common/SegmentedControl';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';
import { cn } from '@/lib/utils';

import { Arriving, FormFooter } from '@/pages/settings/formShell';

import type { SubscriptionPageSettings } from '@/api/types';

const SUPPORT_URL_RE = /^(https?:\/\/[^\s/?#"'<>\\`]+[^\s"'<>\\`]*|tg:\/\/[^\s"'<>\\`]+|mailto:[^\s"'<>\\`]+)$/i;

const text = (max: number) => z.string().max(max);

const schema = z
  .object({
    language: z.enum(['auto', 'ru', 'en']),
    title_ru: text(80),
    title_en: text(80),
    intro_ru: text(500),
    intro_en: text(500),
    tls_button_ru: text(40),
    tls_button_en: text(40),
    tls_note_ru: text(140),
    tls_note_en: text(140),
    web_button_ru: text(40),
    web_button_en: text(40),
    web_note_ru: text(140),
    web_note_en: text(140),
    support_label_ru: text(40),
    support_label_en: text(40),
    support_url: z.string().max(300),
    hide_support: z.boolean(),
    show_fake_tls: z.boolean(),
    show_web: z.boolean(),
    show_backup_domains: z.boolean(),
    show_status: z.boolean(),
    show_qr: z.boolean(),
    hidden_nodes: z.array(z.string()),
  })
  .refine((v) => v.show_fake_tls || v.show_web, { path: ['show_fake_tls'], message: 'links_required' })
  .refine((v) => v.hide_support || v.support_url.trim() === '' || SUPPORT_URL_RE.test(v.support_url.trim()), {
    path: ['support_url'],
    message: 'support_url',
  });

type FormValues = z.infer<typeof schema>;

const DEFAULTS: FormValues = {
  language: 'auto',
  title_ru: '',
  title_en: '',
  intro_ru: '',
  intro_en: '',
  tls_button_ru: '',
  tls_button_en: '',
  tls_note_ru: '',
  tls_note_en: '',
  web_button_ru: '',
  web_button_en: '',
  web_note_ru: '',
  web_note_en: '',
  support_label_ru: '',
  support_label_en: '',
  support_url: '',
  hide_support: false,
  show_fake_tls: true,
  show_web: true,
  show_backup_domains: true,
  show_status: true,
  show_qr: true,
  hidden_nodes: [],
};

type TextLang = 'ru' | 'en';
type TextKey = 'title' | 'intro' | 'tls_button' | 'tls_note' | 'web_button' | 'web_note' | 'support_label';
type TextName = `${TextKey}_${TextLang}`;

interface TextFieldDef {
  key: TextKey;
  max: number;
  label: string;
  standard: string;
  multiline?: boolean;
}

const PAGE_TEXTS: TextFieldDef[] = [
  { key: 'title', max: 80, label: 'settings.subpage_title', standard: 'subpage.title_default' },
  { key: 'intro', max: 500, label: 'settings.subpage_intro', standard: 'subpage.intro_default', multiline: true },
];

const BUTTON_TEXTS: TextFieldDef[] = [
  { key: 'tls_button', max: 40, label: 'settings.subpage_tls_button', standard: 'subpage.tls_button' },
  { key: 'tls_note', max: 140, label: 'settings.subpage_tls_note', standard: 'subpage.tls_note' },
  { key: 'web_button', max: 40, label: 'settings.subpage_web_button', standard: 'subpage.web_button' },
  { key: 'web_note', max: 140, label: 'settings.subpage_web_note', standard: 'subpage.web_note' },
  { key: 'support_label', max: 40, label: 'settings.subpage_support_label', standard: 'subpage.support' },
];

const ALL_TEXTS = [...PAGE_TEXTS, ...BUTTON_TEXTS];

type SwitchField = 'show_fake_tls' | 'show_web' | 'show_backup_domains' | 'show_status' | 'show_qr';

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

function TextField({
  def,
  lang,
  value,
  register,
  disabled,
  onReset,
}: {
  def: TextFieldDef;
  lang: TextLang;
  value: string;
  register: ReturnType<typeof useForm<FormValues>>['register'];
  disabled: boolean;
  onReset: () => void;
}) {
  const { t, i18n } = useTranslation();
  const name: TextName = `${def.key}_${lang}`;
  const id = `subpage-${name}`;
  const tooLong = value.length > def.max;
  const placeholder = i18n.getFixedT(lang)(def.standard);
  const props = {
    id,
    disabled,
    lang,
    placeholder,
    'aria-invalid': tooLong || undefined,
    'aria-describedby': `${id}-hint`,
    ...register(name),
  };
  return (
    <div className="space-y-2">
      <div className="flex min-h-6 items-center justify-between gap-2">
        <Label htmlFor={id}>{t(def.label)}</Label>
        {value !== '' && !disabled && (
          <Button type="button" variant="ghost" size="xs" onClick={onReset}>
            <RotateCcw />
            {t('settings.subpage_reset_text')}
          </Button>
        )}
      </div>
      {def.multiline ? <Textarea rows={3} {...props} /> : <Input {...props} />}
      <p id={`${id}-hint`} className="flex justify-between gap-3 text-label text-mute">
        <span className="text-err">{tooLong && t('settings.subpage_too_long', { max: def.max })}</span>
        <span className={cn('shrink-0 tabular-nums', tooLong && 'text-err')}>
          {t('settings.subpage_count', { count: value.length, max: def.max })}
        </span>
      </p>
    </div>
  );
}

function trimmed(v: FormValues): SubscriptionPageSettings {
  const out = { ...v, support_url: v.support_url.trim() };
  for (const def of ALL_TEXTS) {
    for (const lang of ['ru', 'en'] as const) {
      const name: TextName = `${def.key}_${lang}`;
      out[name] = v[name].trim();
    }
  }
  return out;
}

export function SubscriptionPageForm() {
  const { t, i18n } = useTranslation();
  const { isOwner, isWriter } = useAuth();
  const settingsQuery = useSettings();
  const nodesQuery = useNodes();
  const brandingQuery = useBranding();
  const putSettings = usePutSettings();
  const [src, setSrc] = useState('');
  const [chosenLang, setTextLang] = useState<TextLang | null>(null);
  const disabled = !isOwner;

  const {
    control,
    register,
    reset,
    setValue,
    handleSubmit,
    formState: { isDirty, isSubmitting },
  } = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: DEFAULTS, mode: 'onChange' });

  useEffect(() => {
    const saved = settingsQuery.data?.subscription_page;
    if (saved) reset({ ...DEFAULTS, ...saved });
  }, [settingsQuery.data, reset]);

  const values = useWatch({ control }) as FormValues;
  const savedLang = settingsQuery.data?.subscription_page?.language;
  const textLang: TextLang =
    chosenLang ?? (savedLang === 'ru' || savedLang === 'en' ? savedLang : i18n.language?.startsWith('en') ? 'en' : 'ru');
  const noLinks = !values.show_fake_tls && !values.show_web;
  const textTooLong = ALL_TEXTS.some((def) =>
    (['ru', 'en'] as const).some((lang) => (values[`${def.key}_${lang}`] ?? '').length > def.max),
  );
  const supportURL = (values.support_url ?? '').trim();
  const badSupportURL = !values.hide_support && supportURL !== '' && !SUPPORT_URL_RE.test(supportURL);
  const payload = JSON.stringify({ values, textLang });

  useEffect(() => {
    if (!isWriter || !settingsQuery.data || noLinks) return;
    const handle = window.setTimeout(() => {
      const bytes = new TextEncoder().encode(JSON.stringify(trimmed({ ...DEFAULTS, ...values })));
      const encoded = btoa(Array.from(bytes, (b) => String.fromCharCode(b)).join(''));
      const query = new URLSearchParams({ language: textLang, settings: encoded });
      setSrc(`/api/v1/settings/subscription-page/preview?${query.toString()}`);
    }, 350);
    return () => window.clearTimeout(handle);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [payload, isWriter, settingsQuery.data]);

  const onSubmit = async (v: FormValues) => {
    try {
      await putSettings.mutateAsync({ subscription_page: trimmed(v) });
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
  const shownNodes = nodes.filter((n) => !(values.hidden_nodes ?? []).includes(n.id));
  const noServers = nodes.length > 0 && shownNodes.length === 0;
  const withBackup = shownNodes.filter((n) => (n.tls_domains ?? []).length > 0).length;
  const brandingLink = brandingQuery.data?.support_link ?? '';
  const textField = (def: TextFieldDef) => {
    const name: TextName = `${def.key}_${textLang}`;
    return (
      <TextField
        key={name}
        def={def}
        lang={textLang}
        value={values[name] ?? ''}
        register={register}
        disabled={disabled}
        onReset={() => setValue(name, '', { shouldDirty: true, shouldValidate: true })}
      />
    );
  };

  return (
    <form
      className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(0,440px)]"
      onSubmit={(e) => void handleSubmit(onSubmit)(e)}
      noValidate
    >
      <div className="flex min-w-0 flex-col gap-4">
        <Arriving index={0}>
          <Panel>
            <PanelHeader icon={FileText} title={t('settings.subpage_content')} />
            <PanelBody className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="subpage-language">{t('settings.subpage_language')}</Label>
                <select
                  id="subpage-language"
                  className="ops-select w-full max-w-sm"
                  disabled={disabled}
                  {...register('language', {
                    onChange: (e: { target: { value: string } }) => {
                      if (e.target.value === 'ru' || e.target.value === 'en') setTextLang(e.target.value);
                    },
                  })}
                >
                  <option value="auto">{t('settings.subpage_language_auto')}</option>
                  <option value="ru">{t('settings.panel_telegram_language_ru')}</option>
                  <option value="en">{t('settings.panel_telegram_language_en')}</option>
                </select>
              </div>
              <div className="space-y-4 border-t border-hairline pt-4">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <p className="min-w-0 max-w-[60ch] text-label text-mute">{t('settings.subpage_texts_hint')}</p>
                  <SegmentedControl
                    label={t('settings.subpage_texts_lang')}
                    value={textLang}
                    onChange={setTextLang}
                    options={[
                      { value: 'ru', label: t('settings.subpage_texts_ru') },
                      { value: 'en', label: t('settings.subpage_texts_en') },
                    ]}
                  />
                </div>
                {PAGE_TEXTS.map(textField)}
                <div className="space-y-4 border-t border-hairline pt-4">
                  <div>
                    <h3 className="text-body font-semibold">{t('settings.subpage_buttons')}</h3>
                    <p className="mt-0.5 text-label text-mute">{t('settings.subpage_buttons_hint')}</p>
                  </div>
                  {BUTTON_TEXTS.map(textField)}
                </div>
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
                hint={`${t('settings.subpage_show_backup_hint')} ${
                  withBackup === 0
                    ? t('settings.subpage_backup_none')
                    : t('settings.subpage_backup_count', { count: withBackup, total: shownNodes.length })
                }`}
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
              <SwitchRow name="show_status" label={t('settings.subpage_show_status')} hint={t('settings.subpage_show_status_hint')} control={control} disabled={disabled} />
              <SwitchRow name="show_qr" label={t('settings.subpage_show_qr')} hint={t('settings.subpage_show_qr_hint')} control={control} disabled={disabled} />
            </PanelBody>
          </Panel>
        </Arriving>

        <Arriving index={3}>
          <Panel>
            <PanelHeader icon={LifeBuoy} title={t('settings.subpage_support')} />
            <PanelBody className="space-y-4">
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0">
                  <Label htmlFor="subpage-support">{t('settings.subpage_support_show')}</Label>
                  <p className="mt-0.5 text-label text-mute">{t('settings.subpage_support_show_hint')}</p>
                </div>
                <Controller
                  control={control}
                  name="hide_support"
                  render={({ field }) => (
                    <Switch
                      id="subpage-support"
                      checked={!field.value}
                      onCheckedChange={(on) => field.onChange(!on)}
                      disabled={disabled}
                    />
                  )}
                />
              </div>
              {!values.hide_support && (
                <div className="space-y-2">
                  <Label htmlFor="subpage-support-url">{t('settings.subpage_support_url')}</Label>
                  <Input
                    id="subpage-support-url"
                    className="mono max-w-lg text-mono"
                    inputMode="url"
                    autoComplete="off"
                    spellCheck={false}
                    disabled={disabled}
                    placeholder={brandingLink || 'https://t.me/your_support'}
                    aria-invalid={badSupportURL || undefined}
                    aria-describedby="subpage-support-url-hint"
                    {...register('support_url')}
                  />
                  <p id="subpage-support-url-hint" className={cn('text-label', badSupportURL ? 'text-err' : 'text-mute')}>
                    {badSupportURL
                      ? t('settings.subpage_support_url_invalid')
                      : supportURL !== ''
                        ? t('settings.subpage_support_url_hint')
                        : brandingLink
                          ? t('settings.subpage_support_url_branding', { link: brandingLink })
                          : t('settings.subpage_support_url_none')}
                  </p>
                </div>
              )}
            </PanelBody>
          </Panel>
        </Arriving>

        <Arriving index={4}>
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
                            <span className="mono truncate text-mono text-mute">{n.hostname}</span>
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
            <Button type="submit" disabled={isSubmitting || !isDirty || noLinks || noServers || textTooLong || badSupportURL}>
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
              <p className="text-label text-mute">{t('settings.subpage_preview_hint')}</p>
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
            </PanelBody>
          </Panel>
        </div>
      )}
    </form>
  );
}
