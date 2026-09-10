import { zodResolver } from '@hookform/resolvers/zod';
import { Circle, CircleCheck } from 'lucide-react';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Controller, useForm } from 'react-hook-form';
import { z } from 'zod';

import { useMutation } from '@tanstack/react-query';
import { useCreateNode } from '@/api/nodes';
import { DraftBanner } from '@/components/common/DraftBanner';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { HelpButton } from '@/help';
import { api, ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';
import { cn } from '@/lib/utils';

import type { CSSProperties } from 'react';
import type { CreateNodeResult, NodeEngine } from '@/api/types';

// Mirrors internal/domain/types.go hostRe: lowercase DNS name with a dot, no scheme/path.
const HOSTNAME_RE = /^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$/;

const ENGINES: NodeEngine[] = ['telemt', 'tproxy'];

const FORM_ID = 'create-node-form';

/** internal/api/nodes.go defaultClassicPort. */
const DEFAULT_CLASSIC_PORT = 8443;

const hostname = z
  .string()
  .trim()
  .toLowerCase()
  .min(1)
  .max(253)
  .refine((h) => HOSTNAME_RE.test(h) && h.includes('.'));

const schema = z
  .object({
    name: z.string().trim().min(1),
    hostname,
    acme_email: z.string().trim().email(),
    public_ip: z.string().trim().refine((v) => !v || z.string().ip().safeParse(v).success).optional(),
    engine: z.enum(['telemt', 'tproxy']),
    tls_domain: z.string().trim().toLowerCase(),
    classic_port: z.coerce.number().int(),
  })
  .superRefine((val, ctx) => {
    if (val.engine !== 'telemt') return;
    if (!HOSTNAME_RE.test(val.tls_domain) || !val.tls_domain.includes('.')) {
      ctx.addIssue({ code: 'custom', path: ['tls_domain'], message: 'hostname' });
    }
    if (!Number.isInteger(val.classic_port) || val.classic_port < 1024 || val.classic_port > 65535) {
      ctx.addIssue({ code: 'custom', path: ['classic_port'], message: 'range' });
    }
  });

type FormValues = z.infer<typeof schema>;

const defaultValues: FormValues = {
  name: '',
  hostname: '',
  acme_email: '',
  public_ip: '',
  engine: 'telemt',
  tls_domain: '',
  classic_port: DEFAULT_CLASSIC_PORT,
};

function EngineCards({ value, onChange }: { value: NodeEngine; onChange: (engine: NodeEngine) => void }) {
  const { t } = useTranslation();

  return (
    <div role="radiogroup" aria-label={t('nodes.field_engine')} className="grid grid-cols-1 gap-2 sm:grid-cols-2">
      {ENGINES.map((engine) => {
        const active = value === engine;
        return (
          <button
            key={engine}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onChange(engine)}
            className={cn(
              'rounded-control border px-3 py-3 text-left transition-[background-color,border-color,color,scale] outline-none active:scale-[0.985] focus-visible:ring-2 focus-visible:ring-ring/70',
              active ? 'border-hairline-strong bg-elevated' : 'border-hairline hover:bg-elevated/60',
            )}
          >
            <span className="mono flex items-center gap-2 text-mono text-foreground">
              <span
                className="tgwp-tone-tint flex size-5 shrink-0 items-center justify-center rounded-control border"
                style={{ '--tone': active ? 'var(--brand-primary)' : 'var(--mute)' } as CSSProperties}
                aria-hidden="true"
              >
                {active ? <CircleCheck size={12} strokeWidth={1.8} /> : <Circle size={12} strokeWidth={1.8} />}
              </span>
              {engine}
            </span>
            <span className="mt-1 block text-label text-mute">{t(`nodes.engine_${engine}_desc`)}</span>
          </button>
        );
      })}
    </div>
  );
}

interface CreateNodeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (result: CreateNodeResult) => void;
}

/** Form to register a new node: engine, name, hostname, ACME e-mail, Fake-TLS listener, optional public IP. */
export function CreateNodeDialog({ open, onOpenChange, onCreated }: CreateNodeDialogProps) {
  const { t } = useTranslation();
  const createNode = useCreateNode();
  const [step, setStep] = useState(1);
  const dns = useMutation({ mutationFn: (body: { hostname: string; public_ip?: string }) => api.post<{ addresses: string[]; matches: boolean; error: string }>('/api/v1/nodes/preflight/dns', body) });
  // The Fake-TLS domain follows the hostname until the operator types their own.
  const tlsDomainEdited = useRef(false);

  const {
    control,
    trigger,
    register,
    handleSubmit,
    reset,
    setError,
    setValue,
    watch,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues,
  });

  const values = watch();
  const engine = values.engine;
  const hostnameField = register('hostname');

  const draft = useDraft<FormValues>('node-create', values, { initial: defaultValues, open });

  const closeAndReset = () => {
    tlsDomainEdited.current = false;
    setStep(1);
    dns.reset();
    reset(defaultValues);
  };

  const resumeDraft = () => {
    if (!draft.draft) return;
    const saved = draft.draft.value;
    reset(saved, { keepDefaultValues: true });
    // A Fake-TLS domain that still mirrors the hostname keeps following it.
    tlsDomainEdited.current = saved.tls_domain !== saved.hostname.trim().toLowerCase();
    draft.dismiss();
  };

  const onSubmit = async (values: FormValues) => {
    const telemt = values.engine === 'telemt';
    try {
      const result = await createNode.mutateAsync({
        name: values.name,
        hostname: values.hostname,
        acme_email: values.acme_email,
        public_ip: values.public_ip || undefined,
        engine: values.engine,
        tls_domain: telemt ? values.tls_domain : undefined,
        classic_port: telemt ? values.classic_port : undefined,
      });
      draft.clear();
      closeAndReset();
      onOpenChange(false);
      onCreated(result);
    } catch (err) {
      if (err instanceof ApiError && (err.fields.hostname || err.fields.public_ip || err.fields.acme_email || err.code === 'conflict')) setStep(1);
      if (err instanceof ApiError && err.code === 'conflict') {
        setError('hostname', { message: t('nodes.hostname_exists') });
        return;
      }
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        const known = ['name', 'hostname', 'acme_email', 'public_ip', 'engine', 'tls_domain', 'classic_port'] as const;
        for (const field of Object.keys(err.fields)) {
          if ((known as readonly string[]).includes(field)) {
            setError(field as (typeof known)[number], { message: err.fields[field] });
          }
        }
        return;
      }
      setError('root', { message: t('nodes.create_error') });
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) closeAndReset();
        onOpenChange(next);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="flex items-center gap-1.5">
            <DialogTitle>{t('nodes.create_title')}</DialogTitle>
            <HelpButton topic="nodes.create" className="-my-1.5" />
          </div>
          <DialogDescription>{t('nodes.create_description')}</DialogDescription>
        </DialogHeader>

        <ol className="flex gap-4 border-b border-hairline pb-3 text-label">{[1, 2, 3].map((n) => <li key={n} aria-current={step === n ? 'step' : undefined} className={step === n ? 'font-semibold text-foreground' : 'text-mute'}>{n}. {t(`nodes.wizard_step_${n}`)}</li>)}</ol>
        {draft.draft && <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />}

        <form
          id={FORM_ID}
          className="max-h-[62vh] space-y-4 overflow-y-auto pr-1"
          onSubmit={(e) => { if (step === 1) { e.preventDefault(); void trigger(['name', 'hostname', 'public_ip', 'acme_email']).then((valid) => { if (valid) setStep(2); }); } else { void handleSubmit(onSubmit)(e); } }}
          noValidate
        >
          <div hidden={step !== 1} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="node-name">{t('nodes.field_name')}</Label>
            <Input id="node-name" autoFocus {...register('name')} aria-invalid={!!errors.name} />
            {errors.name && <p className="text-label text-destructive">{t('common.required')}</p>}
          </div>

          <div className="space-y-2">
            <Label htmlFor="node-hostname">{t('nodes.field_hostname')}</Label>
            <Input
              id="node-hostname"
              className="mono"
              placeholder="node1.example.com"
              {...hostnameField}
              onChange={(e) => {
                void hostnameField.onChange(e);
                if (!tlsDomainEdited.current) setValue('tls_domain', e.target.value.trim().toLowerCase());
              }}
              aria-invalid={!!errors.hostname}
            />
            <p className="text-label text-mute">{t('nodes.field_hostname_hint')}</p>
            {errors.hostname && <p className="text-label text-destructive">{t('nodes.validation_hostname')}</p>}
          </div>

          <div className="space-y-2">
            <Label htmlFor="node-acme-email">{t('nodes.field_acme_email')}</Label>
            <Input id="node-acme-email" type="email" {...register('acme_email')} aria-invalid={!!errors.acme_email} />
            {errors.acme_email && <p className="text-label text-destructive">{t('nodes.validation_email')}</p>}
          </div>

          <div className="space-y-2">
            <Label htmlFor="node-public-ip">
              {t('nodes.field_public_ip')}{' '}
              <span className="font-normal text-mute">
                ({t(engine === 'telemt' ? 'nodes.field_public_ip_telemt' : 'nodes.field_public_ip_optional')})
              </span>
            </Label>
            <Input id="node-public-ip" className="mono" {...register('public_ip')} aria-invalid={!!errors.public_ip} />
            {errors.public_ip && (
              <p className="text-label text-destructive">{errors.public_ip.message || t('nodes.validation_ipv4')}</p>
            )}
          </div>

          <Button type="button" variant="outline" disabled={!values.hostname || dns.isPending} onClick={() => dns.mutate({ hostname: values.hostname, public_ip: values.public_ip })}>{t('nodes.wizard_check_dns')}</Button>
          {dns.isError && <p role="alert" className="text-destructive">{dns.error.message}</p>}
          {dns.data && dns.variables?.hostname === values.hostname && dns.variables?.public_ip === values.public_ip && <div role="status" className="space-y-1 text-label"><p className={dns.data.matches ? 'text-ok' : 'text-warn'}>{t(dns.data.matches ? 'nodes.wizard_dns_ok' : 'nodes.wizard_dns_fix')}</p><p className="mono break-all">{values.hostname} → {dns.data.addresses.join(', ') || '—'}</p>{!dns.data.matches && values.public_ip && <p className="mono">A / AAAA · {values.hostname} · {values.public_ip}</p>}</div>}
          </div>
          <div hidden={step !== 2} className="space-y-4">
          <div className="space-y-2">
            <Label>{t('nodes.field_engine')}</Label>
            <Controller
              control={control}
              name="engine"
              render={({ field }) => <EngineCards value={field.value} onChange={field.onChange} />}
            />
            <p className="text-label text-mute">{t(`nodes.engine_${engine}_links`)}</p>
          </div>

          {engine === 'telemt' && (
            <details><summary className="cursor-pointer text-label text-mute">{t('nodes.wizard_advanced')}</summary><div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-[1fr_auto]">
              <div className="space-y-2">
                <Label htmlFor="node-tls-domain">{t('nodes.field_tls_domain')}</Label>
                <Input
                  id="node-tls-domain"
                  className="mono"
                  placeholder="node1.example.com"
                  {...register('tls_domain', { onChange: () => (tlsDomainEdited.current = true) })}
                  aria-invalid={!!errors.tls_domain}
                />
                <p className="text-label text-mute">{t('nodes.field_tls_domain_hint')}</p>
                {errors.tls_domain && <p className="text-label text-destructive">{t('nodes.validation_hostname')}</p>}
              </div>
              <div className="space-y-2">
                <Label htmlFor="node-classic-port">{t('nodes.field_classic_port')}</Label>
                <Input
                  id="node-classic-port"
                  type="number"
                  min={1024}
                  max={65535}
                  className="mono w-28"
                  {...register('classic_port')}
                  aria-invalid={!!errors.classic_port}
                />
                {errors.classic_port && <p className="text-label text-destructive">{t('nodes.validation_classic_port')}</p>}
              </div>
            </div></details>
          )}

          {engine === 'telemt' && <p className="rounded-control bg-elevated p-3 text-label">{t('nodes.wizard_proxy_summary', { port: values.classic_port })}</p>}
          </div>
          {errors.root && (
            <p role="alert" className="flex items-start gap-2 text-body text-destructive">
              <span className="mt-2 size-[7px] shrink-0 rounded-pill bg-destructive" aria-hidden="true" />
              {errors.root.message}
            </p>
          )}
        </form>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={isSubmitting}>
            {t('common.cancel')}
          </Button>
          {step === 2 && <Button type="button" variant="outline" onClick={() => setStep(1)}>{t('nodes.wizard_back')}</Button>}
          <Button type="submit" form={FORM_ID} disabled={isSubmitting}>
            {t(step === 1 ? 'nodes.wizard_next' : 'nodes.create_submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
