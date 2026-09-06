import { ArrowLeft, Link2, Upload, X } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useParams } from 'react-router-dom';

import { useCreateSiteTemplate, useSiteTemplate, useUpdateSiteTemplate, useValidateSiteTemplate } from '@/api/sites';
import { useAuth } from '@/auth/AuthProvider';
import { DraftBanner } from '@/components/common/DraftBanner';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';
import { formatBytes } from '@/lib/format';

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
      <span className={`mt-[6px] size-[7px] shrink-0 rounded-full ${tone === 'err' ? 'bg-err' : 'bg-warn'}`} aria-hidden="true" />
      <span className={`mono min-w-0 flex-1 text-xs break-words ${tone === 'err' ? 'text-foreground' : 'text-mute'}`}>{children}</span>
    </li>
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
    const saved = draft.draft.value;
    setName(saved.name);
    setHtml(saved.html);
    setAssets(saved.assets ?? {});
    setLastValidation(null);
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
        toast.add({ description: t('sites.create_success'), type: 'success' });
        navigate(`/sites/${created.id}`, { replace: true });
      } else {
        await updateTemplate.mutateAsync({ name, html, assets });
        draft.clear();
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
    setAssets((prev) => ({ ...prev, ...Object.fromEntries(entries) }));
  };

  const assetsTotalBytes = useMemo(
    () => Object.values(assets).reduce((sum, b64) => sum + base64ByteSize(b64), 0),
    [assets],
  );

  const removeAsset = (path: string) => {
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
    return <Skeleton className="h-96 w-full" />;
  }

  return (
    <>
      <div className="flex flex-col gap-3">
        <Link to="/sites" className="mono inline-flex w-fit items-center gap-1 text-xs text-dim hover:text-foreground">
          <ArrowLeft className="size-3" />
          {t('sites.title')}
        </Link>

        <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
          <div className="flex min-w-0 items-center gap-1.5">
            <h1 className="min-w-0 truncate text-xl font-semibold tracking-[-0.015em] text-foreground">
              {isNew ? t('sites.editor_title_new') : t('sites.editor_title_edit', { name: loaded?.name ?? name })}
            </h1>
            <HelpButton topic="sites.editor" />
          </div>

          {isWriter && (
            <div className="flex flex-wrap items-center gap-2 lg:justify-end">
              <Button type="button" variant="outline" size="sm" onClick={() => void handleValidate()} disabled={validateTemplate.isPending}>
                {t('sites.validate_button')}
              </Button>
              {!isNew && (
                <Button type="button" variant="outline" size="sm" onClick={() => setAssignOpen(true)}>
                  <Link2 />
                  {t('sites.assign_button')}
                </Button>
              )}
              <Button type="button" size="sm" onClick={() => void handleSave()} disabled={saveDisabled}>
                {t('common.save')}
              </Button>
            </div>
          )}
        </div>
      </div>

      {draft.draft && (
        <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} className="max-w-2xl" />
      )}

      <div className="space-y-1.5">
        <Label htmlFor="tpl-name">{t('sites.field_name')}</Label>
        <Input
          id="tpl-name"
          className="max-w-xs"
          value={name}
          onChange={(e) => setName(e.target.value)}
          disabled={!isWriter || isPreset}
          aria-invalid={name.trim() === ''}
        />
        {isPreset && <p className="text-xs text-mute">{t('sites.preset_name_locked')}</p>}
      </div>

      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-4">
          <Panel>
            <PanelHeader title={t('sites.field_html')} meta={t('sites.editor_lines', { count: lineCount })} />
            <LineNumberedTextarea
              value={html}
              onChange={setHtml}
              aria-label={t('sites.field_html')}
              className={!isWriter ? 'opacity-70' : undefined}
            />
          </Panel>

          <Panel>
            <PanelHeader
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
              <PanelBody className="py-6 text-center text-sm text-mute">{t('sites.assets_empty')}</PanelBody>
            ) : (
              <ul className="divide-y divide-hairline">
                {assetEntries.map(([path, b64]) => (
                  <li key={path} className="flex h-9 items-center gap-3 px-4">
                    <span className="mono min-w-0 flex-1 truncate text-xs text-foreground">{path}</span>
                    <span className="mono shrink-0 text-xs text-dim">{formatBytes(base64ByteSize(b64))}</span>
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
        </div>

        <div className="flex min-w-0 flex-col gap-4">
          <Panel>
            <PanelHeader title={t('sites.preview_title')} />
            {/* The rendered site is someone else's page: it keeps its own white
                ground, framed by the panel rather than themed by it. */}
            <iframe sandbox="" srcDoc={previewSrcdoc} title={t('sites.preview_title')} className="h-[28rem] w-full bg-white" />
          </Panel>

          <Panel>
            <PanelHeader
              title={t('sites.validation_title')}
              meta={lastValidation ? `${lastValidation.errors.length} err · ${lastValidation.warnings.length} warn` : undefined}
            />
            <PanelBody>
              {!lastValidation ? (
                <p className="text-sm text-mute">{t('sites.validate_hint')}</p>
              ) : lastValidation.errors.length === 0 && lastValidation.warnings.length === 0 ? (
                <p className="flex items-center gap-2 text-sm text-foreground">
                  <span className="size-[7px] shrink-0 rounded-full bg-ok" aria-hidden="true" />
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
        </div>
      </div>

      {!isNew && (
        <AssignTemplateDialog open={assignOpen} onOpenChange={setAssignOpen} templateId={id} templateName={loaded?.name ?? name} />
      )}
    </>
  );
}
