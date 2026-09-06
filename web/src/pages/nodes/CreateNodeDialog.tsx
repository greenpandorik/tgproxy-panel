import { zodResolver } from '@hookform/resolvers/zod';
import { useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { Controller, useForm } from 'react-hook-form';
import { z } from 'zod';

import { useCreateNode } from '@/api/nodes';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ApiError } from '@/lib/api';
import { cn } from '@/lib/utils';

import type { CreateNodeResult, NodeEngine } from '@/api/types';

// Mirrors internal/domain/types.go hostRe: lowercase DNS name with a dot, no scheme/path.
const HOSTNAME_RE = /^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$/;

const ENGINES: NodeEngine[] = ['telemt', 'tproxy'];

// The footer sits outside the scrolling field list, so its submit button reaches
// the form by id rather than by nesting.
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
    public_ip: z.string().trim().optional(),
    engine: z.enum(['telemt', 'tproxy']),
    tls_domain: z.string().trim().toLowerCase(),
    classic_port: z.coerce.number().int(),
  })
  .superRefine((val, ctx) => {
    if (val.engine !== 'telemt') return;
    if (!HOSTNAME_RE.test(val.tls_domain) || !val.tls_domain.includes('.')) {
      ctx.addIssue({ code: 'custom', path: ['tls_domain'], message: 'hostname' });
    }
    // internal/api/nodes.go validateClassicPort: unprivileged, and never the two
    // ports Caddy already owns on a telemt node.
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

/**
 * The engine choice, as two cards side by side.
 *
 * It is the one decision on this form that cannot be changed afterwards - a node
 * is installed for one engine - and it decides what the operator can hand out
 * later, so it is not a select. Each card says what the engine gives you in one
 * line; the chosen one is filled with --bg-3 and outlined with the strong
 * hairline, the same "this one is active" treatment the range switch uses.
 */
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
              'rounded-md border px-3 py-2.5 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/70',
              active ? 'border-hairline-strong bg-elevated' : 'border-hairline hover:bg-elevated/60',
            )}
          >
            <span className="mono flex items-center gap-2 text-xs text-foreground">
              <span
                className={cn('size-[7px] shrink-0 rounded-full', active ? 'bg-brand-primary' : 'bg-hairline-strong')}
                aria-hidden="true"
              />
              {engine}
            </span>
            <span className="mt-1 block text-xs text-mute">{t(`nodes.engine_${engine}_desc`)}</span>
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
  // The Fake-TLS domain follows the hostname until the operator types their own -
  // masking behind the node's own site is the sane default, and it would be silly
  // to make them type the same name twice.
  const tlsDomainEdited = useRef(false);

  const {
    control,
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

  const engine = watch('engine');
  const hostnameField = register('hostname');

  const closeAndReset = () => {
    tlsDomainEdited.current = false;
    reset(defaultValues);
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
      closeAndReset();
      onOpenChange(false);
      onCreated(result);
    } catch (err) {
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
          <DialogTitle>{t('nodes.create_title')}</DialogTitle>
          <DialogDescription>{t('nodes.create_description')}</DialogDescription>
        </DialogHeader>

        <form
          id={FORM_ID}
          className="max-h-[62vh] space-y-4 overflow-y-auto pr-1"
          onSubmit={(e) => void handleSubmit(onSubmit)(e)}
          noValidate
        >
          <div className="space-y-1.5">
            <Label htmlFor="node-name">{t('nodes.field_name')}</Label>
            <Input id="node-name" autoFocus {...register('name')} aria-invalid={!!errors.name} />
            {errors.name && <p className="text-xs text-destructive">{t('common.required')}</p>}
          </div>

          <div className="space-y-1.5">
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
            <p className="text-xs text-mute">{t('nodes.field_hostname_hint')}</p>
            {errors.hostname && <p className="text-xs text-destructive">{t('nodes.validation_hostname')}</p>}
          </div>

          <div className="space-y-1.5">
            <Label>{t('nodes.field_engine')}</Label>
            <Controller control={control} name="engine" render={({ field }) => <EngineCards value={field.value} onChange={field.onChange} />} />
            <p className="text-xs text-mute">{t(`nodes.engine_${engine}_links`)}</p>
          </div>

          {engine === 'telemt' && (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_auto]">
              <div className="space-y-1.5">
                <Label htmlFor="node-tls-domain">{t('nodes.field_tls_domain')}</Label>
                <Input
                  id="node-tls-domain"
                  className="mono"
                  placeholder="node1.example.com"
                  {...register('tls_domain', { onChange: () => (tlsDomainEdited.current = true) })}
                  aria-invalid={!!errors.tls_domain}
                />
                <p className="text-xs text-mute">{t('nodes.field_tls_domain_hint')}</p>
                {errors.tls_domain && <p className="text-xs text-destructive">{t('nodes.validation_hostname')}</p>}
              </div>
              <div className="space-y-1.5">
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
                {errors.classic_port && <p className="text-xs text-destructive">{t('nodes.validation_classic_port')}</p>}
              </div>
            </div>
          )}

          <div className="space-y-1.5">
            <Label htmlFor="node-acme-email">{t('nodes.field_acme_email')}</Label>
            <Input id="node-acme-email" type="email" {...register('acme_email')} aria-invalid={!!errors.acme_email} />
            {errors.acme_email && <p className="text-xs text-destructive">{t('nodes.validation_email')}</p>}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="node-public-ip">
              {t('nodes.field_public_ip')}{' '}
              <span className="font-normal text-dim">
                ({t(engine === 'telemt' ? 'nodes.field_public_ip_telemt' : 'nodes.field_public_ip_optional')})
              </span>
            </Label>
            <Input id="node-public-ip" className="mono" {...register('public_ip')} aria-invalid={!!errors.public_ip} />
          </div>

          {errors.root && (
            <p role="alert" className="flex items-start gap-2 text-sm text-destructive">
              <span className="mt-1.5 size-[7px] shrink-0 rounded-full bg-destructive" aria-hidden="true" />
              {errors.root.message}
            </p>
          )}

        </form>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={isSubmitting}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form={FORM_ID} disabled={isSubmitting}>
            {t('nodes.create_submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
