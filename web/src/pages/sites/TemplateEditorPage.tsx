import { ArrowLeft, CircleCheck, Code, Eye, Link2, Loader2, Paperclip, Upload, X } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';

import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useParams } from 'react-router-dom';

import { useCreateSiteTemplate, useSiteTemplate, useUpdateSiteTemplate, useValidateSiteTemplate } from '@/api/sites';
import { useAuth } from '@/auth/AuthProvider';
import { DraftBanner } from '@/components/common/DraftBanner';
import { PageHeader } from '@/components/common/PageHeader';
import { PanelEmpty } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ENTER_CLASS, enterDelay } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';
import { formatBytes } from '@/lib/format';
import { cn } from '@/lib/utils';

import { AssignTemplateDialog } from './AssignTemplateDialog';
import { LineNumberedTextarea } from './LineNumberedTextarea';
import { base64ByteSize, buildPreviewSrcdoc, fileToBase64 } from './templatePreview';

const DEFAULT_HTML = `<!DOCTYPE html>
<html lang="ru">
<head>
  <meta charset="utf-8" />
  <title>Site</title>
  <link rel="stylesheet" href="/style.css">
</head>
<body>
  <h1>Hello</h1>
</body>
</html>
`;

const PREVIEW_DEBOUNCE_MS = 300;

// Client-side guardrails: the backend's JSON body decoder caps requests at 4MB
// (internal/api/respond.go decodeJSON), but that surfaces as an opaque "invalid
// JSON body" once the base64-inflated assets payload (~1.33x the raw bytes) plus
// the HTML pushes the request over that limit. Reject oversized uploads here
// instead, with a message that names the actual limit.
const MAX_ASSET_BYTES = 512 * 1024;
const MAX_ASSETS_TOTAL_BYTES = 2 * 1024 * 1024;

/** Both editor surfaces are the same height, so the two columns end on one line. */
const SURFACE_HEIGHT = 'h-[28rem]';

/** The editable state of the editor, kept as a draft per template id. */
interface TemplateDraft {
  name: string;
  html: string;
  assets: Record<string, string>;
}

const NEW_TEMPLATE: TemplateDraft = { name: '', html: DEFAULT_HTML, assets: {} };

/** Wrapper that forces a full remount on id change (new -> created id, or switching between two existing templates), so local editor state never leaks between templates. */
export function TemplateEditorPage() {
  const { id } = useParams<{ id: string }>();
  return <TemplateEditorInner key={id ?? 'new'} id={id} />;
}

/**
 * One finding from the validator. The dot carries the severity - the same 7px
 * dot the whole panel uses for state - and the text stays in the mono face
 * because it is the parser talking, not the interface.
 */
function Finding({ tone, children }: { tone: 'err' | 'warn'; children: string }) {
  return (
    <li className="flex items-start gap-2">
      <span className={cn('mt-1.5 size-[7px] shrink-0 rounded-pill', tone === 'err' ? 'bg-err' : 'bg-warn')} aria-hidden="true" />
      <span className={cn('mono min-w-0 flex-1 text-mono break-words', tone === 'err' ? 'text-foreground' : 'text-mute')}>
        {children}
      </span>
    </li>
  );
}

/**
 * Panels arrive staggered. `Panel` takes only a className, so the per-element
 * delay rides a wrapper rather than the section itself.
 */
function Arriving({ index, children }: { index: number; children: ReactNode }) {
  return (
    <div className={ENTER_CLASS} style={enterDelay(index)}>
      {children}
    </div>
  );
}

/** The editor in silhouette: the name field, then the two columns of panels it opens into. */
function EditorSkeleton() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-8 w-64" />
      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
        <Skeleton className={cn(SURFACE_HEIGHT, 'w-full rounded-surface')} />
        <Skeleton className={cn(SURFACE_HEIGHT, 'w-full rounded-surface')} />
      </div>
    </div>
  );
}

function TemplateEditorInner({ id }: { id?: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { isWriter } = useAuth();
  const isNew = !id;

  const templateQuery = useSiteTemplate(id ?? '');
  const createTemplate = useCreateSiteTemplate();
  const updateTemplate = useUpdateSiteTemplate(id ?? '');
  const validateTemplate = useValidateSiteTemplate();

  const [name, setName] = useState('');
  const [html, setHtml] = useState(isNew ? DEFAULT_HTML : '');
  const [assets, setAssets] = useState<Record<string, string>>({});
  const [lastValidation, setLastValidation] = useState<{ errors: string[]; warnings: string[] } | null>(null);
  const [assignOpen, setAssignOpen] = useState(false);
  const [previewHtml, setPreviewHtml] = useState(html);
  // Whether the last save landed. It is cleared by the next edit, so the "saved"
  // stamp beside the title always describes what is on screen right now and
  // never outlives the state it is vouching for.
  const [saved, setSaved] = useState(false);

  const loaded = templateQuery.data;
  const isPreset = !!loaded?.is_preset;

  // Adjust local editable state when the template finishes loading, following
  // React's "adjust state during render" pattern (see ThemeProvider's
  // appliedDefault) instead of a setState-in-effect cascade. `syncedId` guards
  // this to run exactly once per (remounted-per-id) editor instance.
  const [syncedId, setSyncedId] = useState<string | undefined>(undefined);
  if (loaded && loaded.id !== syncedId) {
    setSyncedId(loaded.id);
    setName(loaded.name);
    setHtml(loaded.html ?? '');
    setAssets(loaded.assets ?? {});
  }

  // The draft waits for an existing template to load, so the empty editor is
  // never mistaken for edits; a new template starts from the boilerplate.
  const draft = useDraft<TemplateDraft>(
    `site-template-${id ?? 'new'}`,
    { name, html, assets },
    {
      initial: isNew ? NEW_TEMPLATE : { name: loaded?.name ?? '', html: loaded?.html ?? '', assets: loaded?.assets ?? {} },
      open: isWriter && (isNew || !!loaded),
    },
  );

  const resumeDraft = () => {
    if (!draft.draft) return;
    // Named for what it is - the stored draft - so it stops shadowing the
    // `saved` state flag two scopes up, which means something else entirely.
    const draftValue = draft.draft.value;
    setName(draftValue.name);
    setHtml(draftValue.html);
    setAssets(draftValue.assets ?? {});
    setLastValidation(null);
    setSaved(false);
    draft.dismiss();
  };

  useEffect(() => {
    const timer = window.setTimeout(() => setPreviewHtml(html), PREVIEW_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [html]);

  const previewSrcdoc = useMemo(() => buildPreviewSrcdoc(previewHtml, assets), [previewHtml, assets]);

  const handleValidate = async () => {
    try {
      const result = await validateTemplate.mutateAsync({ name, html, assets });
      // The backend serializes a nil Go slice as JSON null (not []) when there are no
      // errors/warnings, so normalize before storing - callers rely on .length/.map.
      const errors = result.report.errors ?? [];
      const warnings = result.report.warnings ?? [];
      setLastValidation({ errors, warnings });
      if (errors.length === 0) {
        toast.add({ description: t('sites.validate_success'), type: 'success' });
      }
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleSave = async () => {
    try {
      if (isNew) {
        const created = await createTemplate.mutateAsync({ name, html, assets });
        draft.clear();
        setSaved(true);
        toast.add({ description: t('sites.create_success'), type: 'success' });
        navigate(`/sites/${created.id}`, { replace: true });
      } else {
        await updateTemplate.mutateAsync({ name, html, assets });
        draft.clear();
        setSaved(true);
        toast.add({ description: t('sites.save_success'), type: 'success' });
      }
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleFiles = async (files: FileList) => {
    const existingTotal = Object.values(assets).reduce((sum, b64) => sum + base64ByteSize(b64), 0);
    let runningTotal = existingTotal;
    const accepted: File[] = [];

    for (const file of Array.from(files)) {
      if (file.size > MAX_ASSET_BYTES) {
        toast.add({
          description: t('sites.asset_too_large', { name: file.name, max: formatBytes(MAX_ASSET_BYTES) }),
          type: 'error',
        });
        continue;
      }
      if (runningTotal + file.size > MAX_ASSETS_TOTAL_BYTES) {
        toast.add({
          description: t('sites.assets_total_too_large', { max: formatBytes(MAX_ASSETS_TOTAL_BYTES) }),
          type: 'error',
        });
        continue;
      }
      runningTotal += file.size;
      accepted.push(file);
    }

    if (accepted.length === 0) return;
    const entries = await Promise.all(accepted.map(async (f) => [f.name, await fileToBase64(f)] as const));
    setSaved(false);
    setAssets((prev) => ({ ...prev, ...Object.fromEntries(entries) }));
  };

  const assetsTotalBytes = useMemo(() => Object.values(assets).reduce((sum, b64) => sum + base64ByteSize(b64), 0), [assets]);

  const removeAsset = (path: string) => {
    setSaved(false);
    setAssets((prev) => {
      const next = { ...prev };
      delete next[path];
      return next;
    });
  };

  const saving = createTemplate.isPending || updateTemplate.isPending;
  const hasErrors = (lastValidation?.errors.length ?? 0) > 0;
  const saveDisabled = saving || name.trim() === '' || hasErrors;
  const lineCount = html.length === 0 ? 1 : html.split('\n').length;
  const assetEntries = Object.entries(assets);

  if (!isNew && templateQuery.isLoading) {
    return <EditorSkeleton />;
  }

  if (!isNew && templateQuery.isError) {
    return (
      <ErrorState
        message={templateQuery.error instanceof ApiError ? templateQuery.error.message : t('common.error_generic')}
        retryLabel={t('common.refresh')}
        onRetry={() => void templateQuery.refetch()}
      />
    );
  }

  return (
    <>
      <div className="flex flex-col gap-3">
        <Link
          to="/sites"
          className="inline-flex w-fit items-center gap-1 text-label text-mute transition-colors hover:text-foreground"
        >
          <ArrowLeft className="size-3.5" />
          {t('sites.title')}
        </Link>

        <PageHeader
          title={isNew ? t('sites.editor_title_new') : t('sites.editor_title_edit', { name: loaded?.name ?? name })}
          description={
            // The save state lives beside the title rather than only in a toast:
            // a toast is gone in four seconds, and "is my work on the server?" is
            // a question the editor is asked all the way through a session.
            saved && !saving ? (
              <span className="flex items-center gap-1.5">
                <span className="size-[7px] shrink-0 rounded-pill bg-ok" aria-hidden="true" />
                {t('sites.save_success')}
              </span>
            ) : undefined
          }
          actions={
            <>
              <HelpButton topic="sites.editor" />
              {isWriter && (
                <>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => void handleValidate()}
                    disabled={validateTemplate.isPending}
                  >
                    {validateTemplate.isPending && <Loader2 className="animate-spin" aria-hidden="true" />}
                    {t('sites.validate_button')}
                  </Button>
                  {!isNew && (
                    <Button type="button" variant="outline" size="sm" onClick={() => setAssignOpen(true)}>
                      <Link2 />
                      {t('sites.assign_button')}
                    </Button>
                  )}
                  <Button type="button" size="sm" onClick={() => void handleSave()} disabled={saveDisabled} aria-busy={saving}>
                    {saving && <Loader2 className="animate-spin" aria-hidden="true" />}
                    {t('common.save')}
                  </Button>
                </>
              )}
            </>
          }
        />
      </div>

      {draft.draft && (
        <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} className="max-w-2xl" />
      )}

      <div className="space-y-2">
        <Label htmlFor="tpl-name">{t('sites.field_name')}</Label>
        <Input
          id="tpl-name"
          className="max-w-xs"
          value={name}
          onChange={(e) => {
            setSaved(false);
            setName(e.target.value);
          }}
          disabled={!isWriter || isPreset}
          aria-invalid={name.trim() === ''}
          aria-describedby={isPreset ? 'tpl-name-hint' : undefined}
        />
        {isPreset && (
          <p id="tpl-name-hint" className="text-label text-mute">
            {t('sites.preset_name_locked')}
          </p>
        )}
      </div>

      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-4">
          <Arriving index={0}>
            <Panel>
              <PanelHeader icon={Code} title={t('sites.field_html')} meta={t('sites.editor_lines', { count: lineCount })} />
              <PanelBody>
                <LineNumberedTextarea
                  value={html}
                  onChange={(next) => {
                    setSaved(false);
                    setHtml(next);
                  }}
                  aria-label={t('sites.field_html')}
                  className={cn(SURFACE_HEIGHT, !isWriter && 'opacity-70')}
                />
              </PanelBody>
            </Panel>
          </Arriving>

          <Arriving index={1}>
            <Panel>
              <PanelHeader
                icon={Paperclip}
                title={t('sites.assets_title')}
                meta={assetEntries.length > 0 ? formatBytes(assetsTotalBytes) : undefined}
                actions={
                  isWriter && (
                    <Button type="button" variant="outline" size="sm" render={<label className="cursor-pointer" />}>
                      <Upload />
                      {t('sites.assets_upload')}
                      <input
                        type="file"
                        multiple
                        className="sr-only"
                        onChange={(e) => {
                          if (e.target.files) void handleFiles(e.target.files);
                          e.target.value = '';
                        }}
                      />
                    </Button>
                  )
                }
              />
              {assetEntries.length === 0 ? (
                <PanelEmpty>{t('sites.assets_empty')}</PanelEmpty>
              ) : (
                <ul className="divide-y divide-hairline">
                  {assetEntries.map(([path, b64]) => (
                    <li key={path} className="flex h-11 items-center gap-3 px-4">
                      <span className="mono min-w-0 flex-1 truncate text-mono text-foreground">{path}</span>
                      <span className="mono shrink-0 text-mono text-mute">{formatBytes(base64ByteSize(b64))}</span>
                      {isWriter && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-xs"
                          className="-mr-1.5 shrink-0"
                          onClick={() => removeAsset(path)}
                          aria-label={t('common.remove')}
                        >
                          <X />
                        </Button>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </Panel>
          </Arriving>
        </div>

        <div className="flex min-w-0 flex-col gap-4">
          <Arriving index={2}>
            <Panel>
              <PanelHeader icon={Eye} title={t('sites.preview_title')} />
              <PanelBody>
                {/* The rendered site is someone else's page: it keeps its own white
                  ground, framed by the panel rather than themed by it. The wrapper
                  carries the radius because an iframe does not clip its own. */}
                <div className={cn(SURFACE_HEIGHT, 'overflow-hidden rounded-control border border-hairline-strong bg-white')}>
                  <iframe sandbox="" srcDoc={previewSrcdoc} title={t('sites.preview_title')} className="size-full" />
                </div>
              </PanelBody>
            </Panel>
          </Arriving>

          <Arriving index={3}>
            <Panel>
              <PanelHeader
                icon={CircleCheck}
                title={t('sites.validation_title')}
                meta={lastValidation ? `${lastValidation.errors.length} err · ${lastValidation.warnings.length} warn` : undefined}
              />
              <PanelBody>
                {validateTemplate.isPending ? (
                  <ul className="space-y-2">
                    <Skeleton className="h-3 w-3/4" />
                    <Skeleton className="h-3 w-1/2" />
                  </ul>
                ) : !lastValidation ? (
                  <p className="text-body text-mute">{t('sites.validate_hint')}</p>
                ) : lastValidation.errors.length === 0 && lastValidation.warnings.length === 0 ? (
                  <p className="flex items-center gap-2 text-body text-foreground">
                    <span className="size-[7px] shrink-0 rounded-pill bg-ok" aria-hidden="true" />
                    {t('sites.validate_success')}
                  </p>
                ) : (
                  <ul className="space-y-2">
                    {lastValidation.errors.map((e, i) => (
                      <Finding key={`e${i}`} tone="err">
                        {e}
                      </Finding>
                    ))}
                    {lastValidation.warnings.map((w, i) => (
                      <Finding key={`w${i}`} tone="warn">
                        {w}
                      </Finding>
                    ))}
                  </ul>
                )}
              </PanelBody>
            </Panel>
          </Arriving>
        </div>
      </div>

      {!isNew && (
        <AssignTemplateDialog
          open={assignOpen}
          onOpenChange={setAssignOpen}
          templateId={id}
          templateName={loaded?.name ?? name}
        />
      )}
    </>
  );
}
