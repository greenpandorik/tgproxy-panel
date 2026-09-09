import { zodResolver } from '@hookform/resolvers/zod';
import { Code, Image, Palette, Type, Upload } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { useUpdateBranding, useUploadBrandingAsset } from '@/api/branding';
import { DraftBanner } from '@/components/common/DraftBanner';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';
import { cn } from '@/lib/utils';
import { useTheme } from '@/theme/ThemeProvider';

import { Arriving, FormFooter } from './formShell';

import type { BrandingAssetKind, BrandingProfile } from '@/api/types';
import { DEFAULT_ACCENT_COLOR, DEFAULT_PRIMARY_COLOR } from '@/components/brand/brand';

const HEX_COLOR_RE = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i;

const schema = z.object({
  panel_name: z.string().trim().min(1),
  primary_color: z.string().regex(HEX_COLOR_RE),
  accent_color: z.string().regex(HEX_COLOR_RE),
  theme_default: z.enum(['dark', 'light']),
  login_text: z.string(),
  support_link: z.string().refine((v) => v === '' || /^https?:\/\//.test(v)),
  footer_text: z.string(),
  custom_css: z.string(),
});

type FormValues = z.infer<typeof schema>;

function valuesFromProfile(p: BrandingProfile): FormValues {
  return {
    panel_name: p.panel_name,
    primary_color: p.primary_color,
    accent_color: p.accent_color,
    theme_default: p.theme_default,
    login_text: p.login_text,
    support_link: p.support_link,
    footer_text: p.footer_text,
    custom_css: p.custom_css,
  };
}

const ASSET_ACCEPT = 'image/png,image/jpeg,image/x-icon,image/svg+xml,.ico,.svg';

/** Mirrors maxBrandingAssetSize in internal/api/branding.go. */
const MAX_ASSET_BYTES = 1.5 * 1024 * 1024;

const ASSET_EXTENSIONS = ['.png', '.jpg', '.jpeg', '.ico', '.svg'];

function assetRejection(file: File): 'size' | 'type' | null {
  if (file.size > MAX_ASSET_BYTES) return 'size';
  const name = file.name.toLowerCase();
  if (!ASSET_EXTENSIONS.some((ext) => name.endsWith(ext))) return 'type';
  return null;
}

function AssetUpload({
  label,
  hint,
  currentUrl,
  onUpload,
  uploading,
}: {
  label: string;
  hint: string;
  currentUrl: string;
  onUpload: (file: File) => void;
  uploading: boolean;
}) {
  const { t } = useTranslation();
  const fileInput = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const previewUrl = currentUrl;

  // dragenter/dragleave fire per child, so count depth to keep the highlight steady.
  const depth = useRef(0);

  const take = (files: FileList | null) => {
    const file = files?.[0];
    if (file) onUpload(file);
  };

  return (
    <div
      onDragEnter={(e) => {
        e.preventDefault();
        depth.current += 1;
        if (!uploading) setDragging(true);
      }}
      onDragOver={(e) => e.preventDefault()}
      onDragLeave={() => {
        depth.current -= 1;
        if (depth.current <= 0) {
          depth.current = 0;
          setDragging(false);
        }
      }}
      onDrop={(e) => {
        e.preventDefault();
        depth.current = 0;
        setDragging(false);
        if (!uploading) take(e.dataTransfer.files);
      }}
      className={cn(
        'grid grid-cols-[36px_minmax(0,1fr)] items-center gap-3 rounded-control border bg-background p-3 transition-colors sm:flex',
        dragging ? 'border-dashed border-brand-primary bg-brand-primary/5' : 'border-hairline',
      )}
    >
      <div className="flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-control border border-hairline bg-white">
        {previewUrl ? (
          <img src={previewUrl} alt={label} className="max-h-full max-w-full object-contain" />
        ) : (
          <span className="mono text-mono text-dim">—</span>
        )}
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-body text-foreground">{label}</p>
        <p className="text-label text-mute">{dragging ? t('settings.branding_upload_drop') : hint}</p>
      </div>
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={uploading}
        className="col-start-2 justify-self-start"
        aria-label={`${t('settings.branding_upload_action')}: ${label}`}
        onClick={() => fileInput.current?.click()}
      >
        <Upload />
        {uploading ? t('settings.branding_upload_busy') : t('settings.branding_upload_action')}
      </Button>
      <input
        ref={fileInput}
        aria-label={label}
        disabled={uploading}
        type="file"
        accept={ASSET_ACCEPT}
        className="sr-only"
        onChange={(e) => {
          take(e.target.files);
          e.target.value = '';
        }}
      />
    </div>
  );
}

/** Hex field: a hairline swatch that opens the OS picker, and the value itself in mono. */
function ColorField({
  id,
  label,
  value,
  onChange,
  invalid,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (next: string) => void;
  invalid: boolean;
}) {
  return (
    <div className="flex items-center gap-2">
      <input
        type="color"
        value={HEX_COLOR_RE.test(value) ? value : '#000000'}
        onChange={(e) => onChange(e.target.value)}
        className="size-8 shrink-0 cursor-pointer rounded-control border border-hairline-strong bg-background p-1 [&::-webkit-color-swatch-wrapper]:p-0 [&::-webkit-color-swatch]:rounded-control [&::-webkit-color-swatch]:border-0"
        aria-label={label}
      />
      <Input id={id} value={value} onChange={(e) => onChange(e.target.value)} className="mono text-mono" aria-invalid={invalid} />
    </div>
  );
}

interface BrandingFormProps {
  // Profile this form edits.
  profile: BrandingProfile;
  onDirtyChange?: (dirty: boolean) => void;
}

export function BrandingForm({ profile, onDirtyChange }: BrandingFormProps) {
  const { t } = useTranslation();
  const { previewBranding } = useTheme();
  const updateBranding = useUpdateBranding(profile.id);
  const uploadAsset = useUploadBrandingAsset(profile.id);

  const [cssRemoved, setCssRemoved] = useState<string[] | null>(null);
  const [uploadingKind, setUploadingKind] = useState<BrandingAssetKind | null>(null);

  const {
    control,
    register,
    handleSubmit,
    reset,
    watch,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      panel_name: '',
      primary_color: DEFAULT_PRIMARY_COLOR,
      accent_color: DEFAULT_ACCENT_COLOR,
      theme_default: 'dark',
      login_text: '',
      support_link: '',
      footer_text: '',
      custom_css: '',
    },
  });

  useEffect(() => {
    onDirtyChange?.(isDirty);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isDirty]);

  const isDirtyRef = useRef(isDirty);
  useEffect(() => {
    isDirtyRef.current = isDirty;
  }, [isDirty]);

  const lastResetProfileIdRef = useRef<string | null>(null);
  useEffect(() => {
    if (lastResetProfileIdRef.current !== profile.id || !isDirtyRef.current) {
      reset(valuesFromProfile(profile));
    }
    lastResetProfileIdRef.current = profile.id;
  }, [profile, reset]);

  const watched = watch();

  // One draft per profile.
  const draft = useDraft<FormValues>(`branding-${profile.id}`, watched, { initial: valuesFromProfile(profile) });

  const resumeDraft = () => {
    if (!draft.draft) return;
    // The saved profile stays the baseline, so the form is dirty and Save enables.
    reset(draft.draft.value, { keepDefaultValues: true });
    draft.dismiss();
  };

  useEffect(() => {
    previewBranding({
      panel_name: watched.panel_name,
      logo_url: profile.logo_url,
      logo_dark_url: profile.logo_dark_url,
      favicon_url: profile.favicon_url,
      primary_color: watched.primary_color,
      accent_color: watched.accent_color,
      theme_default: watched.theme_default,
      custom_css: watched.custom_css,
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    watched.panel_name,
    watched.primary_color,
    watched.accent_color,
    watched.theme_default,
    watched.custom_css,
    profile.favicon_url,
    profile.logo_url,
    profile.logo_dark_url,
  ]);

  useEffect(() => () => previewBranding(null), [previewBranding]);

  const handleReset = () => {
    // An explicit reset is the operator discarding the edits, draft included.
    draft.clear();
    reset(valuesFromProfile(profile));
    setCssRemoved(null);
  };

  const handleUpload = async (kind: BrandingAssetKind, file: File) => {
    const rejection = assetRejection(file);
    if (rejection) {
      toast.add({
        description: t(rejection === 'size' ? 'settings.branding_upload_too_large' : 'settings.branding_upload_bad_type'),
        type: 'error',
      });
      return;
    }
    setUploadingKind(kind);
    try {
      await uploadAsset.mutateAsync({ kind, file });
      toast.add({ description: t('settings.branding_upload_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    } finally {
      setUploadingKind(null);
    }
  };

  const onSubmit = async (values: FormValues) => {
    try {
      const result = await updateBranding.mutateAsync({ name: profile.name, ...values });
      draft.clear();
      setCssRemoved(result.css_removed ?? []);
      reset(valuesFromProfile(result));
      lastResetProfileIdRef.current = result.id;
      toast.add({ description: t('settings.branding_save_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <div className="grid items-start gap-6 2xl:grid-cols-[minmax(0,1fr)_300px]">
      <form className="flex min-w-0 flex-col gap-4" onSubmit={(e) => void handleSubmit(onSubmit)(e)} noValidate>
        {draft.draft && <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />}

        <Arriving>
          <Panel>
            <PanelHeader icon={Image} title={profile.name} meta={t('settings.branding_preview_note')} />
            <PanelBody className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="branding-panel-name">{t('settings.branding_panel_name')}</Label>
                <Input
                  id="branding-panel-name"
                  maxLength={100}
                  {...register('panel_name')}
                  aria-invalid={!!errors.panel_name}
                  aria-describedby={errors.panel_name ? 'branding-panel-name-error' : undefined}
                />
                {errors.panel_name && (
                  <p id="branding-panel-name-error" className="text-label text-destructive">
                    {t('common.required')}
                  </p>
                )}
              </div>

              <p className="text-label text-mute">{t('settings.branding_assets_immediate')}</p>
              <div className="space-y-2">
                <AssetUpload
                  label={t('settings.branding_logo')}
                  hint={t('settings.branding_upload_hint')}
                  currentUrl={profile.logo_url}
                  uploading={uploadingKind !== null}
                  onUpload={(f) => void handleUpload('logo', f)}
                />
                <AssetUpload
                  label={t('settings.branding_logo_dark')}
                  hint={t('settings.branding_logo_dark_hint')}
                  currentUrl={profile.logo_dark_url || ''}
                  uploading={uploadingKind !== null}
                  onUpload={(f) => void handleUpload('logo_dark', f)}
                />
                <AssetUpload
                  label={t('settings.branding_favicon')}
                  hint={t('settings.branding_upload_hint')}
                  currentUrl={profile.favicon_url}
                  uploading={uploadingKind !== null}
                  onUpload={(f) => void handleUpload('favicon', f)}
                />
                <AssetUpload
                  label={t('settings.branding_login_bg')}
                  hint={t('settings.branding_upload_hint')}
                  currentUrl={profile.login_bg_url}
                  uploading={uploadingKind !== null}
                  onUpload={(f) => void handleUpload('login_bg', f)}
                />
              </div>
            </PanelBody>
          </Panel>
        </Arriving>

        <Arriving index={1}>
          <Panel>
            <PanelHeader
              icon={Palette}
              title={t('settings.branding_section_appearance')}
              actions={<HelpButton topic="settings.branding" />}
            />
            <PanelBody className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="branding-primary">{t('settings.branding_primary_color')}</Label>
                <Controller
                  control={control}
                  name="primary_color"
                  render={({ field }) => (
                    <ColorField
                      id="branding-primary"
                      label={t('settings.branding_primary_color')}
                      value={field.value}
                      onChange={field.onChange}
                      invalid={!!errors.primary_color}
                    />
                  )}
                />
                {errors.primary_color && <p className="text-label text-destructive">{t('settings.branding_color_invalid')}</p>}
              </div>

              <div className="space-y-2">
                <Label htmlFor="branding-accent">{t('settings.branding_accent_color')}</Label>
                <Controller
                  control={control}
                  name="accent_color"
                  render={({ field }) => (
                    <ColorField
                      id="branding-accent"
                      label={t('settings.branding_accent_color')}
                      value={field.value}
                      onChange={field.onChange}
                      invalid={!!errors.accent_color}
                    />
                  )}
                />
                {errors.accent_color && <p className="text-label text-destructive">{t('settings.branding_color_invalid')}</p>}
              </div>

              <div className="space-y-2">
                <Label htmlFor="branding-theme">{t('settings.branding_theme_default')}</Label>
                <Controller
                  control={control}
                  name="theme_default"
                  render={({ field }) => (
                    <Select value={field.value} onValueChange={(v) => field.onChange(v ?? 'dark')}>
                      <SelectTrigger id="branding-theme" className="w-full">
                        {/* SelectValue only resolves a matched item's label once the popup has
                        mounted at least once; render the label explicitly so it's correct
                        from first paint (see base-ui select/value/SelectValue.js). */}
                        <SelectValue>
                          {(v: string) => (v === 'light' ? t('common.theme_light') : t('common.theme_dark'))}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="dark">{t('common.theme_dark')}</SelectItem>
                        <SelectItem value="light">{t('common.theme_light')}</SelectItem>
                      </SelectContent>
                    </Select>
                  )}
                />
              </div>
            </PanelBody>
          </Panel>
        </Arriving>

        <Arriving index={2}>
          <Panel>
            <PanelHeader icon={Type} title={t('settings.branding_section_text')} />
            <PanelBody className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="branding-login-text">{t('settings.branding_login_text')}</Label>
                <Textarea id="branding-login-text" rows={2} {...register('login_text')} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="branding-support-link">{t('settings.branding_support_link')}</Label>
                <Input
                  id="branding-support-link"
                  className="mono text-mono"
                  placeholder="https://"
                  {...register('support_link')}
                  aria-invalid={!!errors.support_link}
                  aria-describedby={errors.support_link ? 'branding-support-link-error' : undefined}
                />
                {errors.support_link && (
                  <p id="branding-support-link-error" className="text-label text-destructive">
                    {t('settings.branding_support_link_invalid')}
                  </p>
                )}
              </div>

              <div className="space-y-2">
                <Label htmlFor="branding-footer-text">{t('settings.branding_footer_text')}</Label>
                <Input id="branding-footer-text" {...register('footer_text')} />
              </div>
            </PanelBody>
          </Panel>
        </Arriving>

        <details className="rounded-surface border border-hairline-strong bg-card">
          <summary className="cursor-pointer px-5 py-4 text-body font-medium">{t('settings.branding_advanced')}</summary>
          <Panel>
            <PanelHeader icon={Code} title={t('settings.branding_custom_css')} />
            <PanelBody className="space-y-4">
              <Textarea
                id="branding-custom-css"
                aria-label={t('settings.branding_custom_css')}
                rows={8}
                className="mono text-mono"
                {...register('custom_css')}
              />

              {cssRemoved && cssRemoved.length > 0 && (
                <div className="space-y-2">
                  <p className="text-label text-mute">{t('settings.branding_css_removed_title')}</p>
                  <ul className="space-y-1">
                    {cssRemoved.map((r, i) => (
                      <li key={i} className="flex items-start gap-2 rounded-control border border-hairline px-3 py-2">
                        <span className="mt-1.5 size-[7px] shrink-0 rounded-pill bg-warn" aria-hidden="true" />
                        <span className="mono min-w-0 flex-1 text-mono break-words text-mute">{r}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </PanelBody>
          </Panel>
        </details>

        <FormFooter>
          <Button type="button" variant="outline" onClick={handleReset} disabled={!isDirty}>
            {t('common.reset')}
          </Button>
          <Button type="submit" disabled={isSubmitting || !isDirty}>
            {t('common.save')}
          </Button>
        </FormFooter>
      </form>
      <aside className="sticky top-0 hidden space-y-3 2xl:block" aria-label={t('settings.branding_preview')}>
        <h3 className="text-title">{t('settings.branding_preview')}</h3>
        <p className="text-label text-mute">{t('settings.branding_preview_hint')}</p>
        {(['light', 'dark'] as const).map((mode) => {
          const logo = mode === 'dark' ? profile.logo_dark_url || profile.logo_url : profile.logo_url;
          return (
            <div
              key={mode}
              data-theme={mode}
              className="branding-preview overflow-hidden rounded-surface border border-hairline-strong"
            >
              <div className="preview-header flex min-h-16 items-center gap-3 px-4">
                {logo && <img src={logo} alt="" className="size-8 object-contain" />}
                <span className="truncate text-body font-semibold">{watched.panel_name || profile.panel_name}</span>
              </div>
              <div className="preview-body space-y-4 p-4">
                <span className="text-label">{t(mode === 'light' ? 'common.theme_light' : 'common.theme_dark')}</span>
                <div
                  className="h-2 w-3/4 rounded-control opacity-70"
                  style={{ background: HEX_COLOR_RE.test(watched.primary_color) ? watched.primary_color : undefined }}
                />
                <div
                  className="h-2 w-1/2 rounded-control opacity-70"
                  style={{ background: HEX_COLOR_RE.test(watched.accent_color) ? watched.accent_color : undefined }}
                />
                <p className="text-label break-words">{watched.login_text || t('settings.branding_section_text')}</p>
              </div>
            </div>
          );
        })}
      </aside>
    </div>
  );
}
