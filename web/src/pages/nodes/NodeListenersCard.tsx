import { zodResolver } from '@hookform/resolvers/zod';
import { ExternalLink, Network } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { useNodeRegistrationSecret, usePatchNode } from '@/api/nodes';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { CopyButton } from '@/components/common/CopyButton';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';

import type { Node } from '@/api/types';

// Mirrors internal/domain/types.go hostRe.
const HOSTNAME_RE = /^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$/;
// Four dotted decimal octets, each 0..255 - what validatePublicIP on the server accepts.
const IPV4_RE = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;
const AD_TAG_RE = /^[0-9a-f]{32}$/;

const schema = z
  .object({
    tls_domain: z.string().trim().toLowerCase(),
    classic_port: z.coerce.number().int(),
    public_ip: z.string().trim(),
    ad_tag: z.string().trim().toLowerCase(),
  })
  .superRefine((val, ctx) => {
    if (!HOSTNAME_RE.test(val.tls_domain) || !val.tls_domain.includes('.')) {
      ctx.addIssue({ code: 'custom', path: ['tls_domain'], message: 'hostname' });
    }
    if (!Number.isInteger(val.classic_port) || val.classic_port < 1024 || val.classic_port > 65535) {
      ctx.addIssue({ code: 'custom', path: ['classic_port'], message: 'range' });
    }
    // Empty stays allowed (the installer fills it in); anything else must be one IPv4.
    if (val.public_ip !== '' && !IPV4_RE.test(val.public_ip)) {
      ctx.addIssue({ code: 'custom', path: ['public_ip'], message: 'ipv4' });
    }
    if (val.ad_tag !== '' && !AD_TAG_RE.test(val.ad_tag)) {
      ctx.addIssue({ code: 'custom', path: ['ad_tag'], message: 'ad_tag' });
    }
  });

type FormValues = z.infer<typeof schema>;

type Pending = { kind: 'fake_tls' | 'public_ip'; values: FormValues } | null;

const valuesOf = (node: Node): FormValues => ({
  tls_domain: node.tls_domain,
  classic_port: node.classic_port,
  public_ip: node.public_ip,
  ad_tag: node.ad_tag,
});

// The telemt node's addresses: the public IP it is reachable at, and the Fake-TLS listener.
export function NodeListenersCard({ node, canEdit }: { node: Node; canEdit: boolean }) {
  const { t } = useTranslation();
  const patchNode = usePatchNode(node.id);
  const registrationSecret = useNodeRegistrationSecret(node.id, canEdit);
  const [pending, setPending] = useState<Pending>(null);

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: valuesOf(node),
  });

  useEffect(() => {
    reset({ tls_domain: node.tls_domain, classic_port: node.classic_port, public_ip: node.public_ip, ad_tag: node.ad_tag });
  }, [node.tls_domain, node.classic_port, node.public_ip, node.ad_tag, reset]);

  const askToSave = (values: FormValues) => {
    const fakeTLS = values.tls_domain !== node.tls_domain || values.classic_port !== node.classic_port;
    setPending({ kind: fakeTLS ? 'fake_tls' : 'public_ip', values });
  };

  const onSubmit = async (values: FormValues) => {
    try {
      await patchNode.mutateAsync({
        tls_domain: values.tls_domain,
        classic_port: values.classic_port,
        public_ip: values.public_ip,
        ad_tag: values.ad_tag,
      });
      reset(values);
      toast.add({ description: t('nodes.listeners_saved'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <Panel>
      <PanelHeader icon={Network} title={t('nodes.listeners_title')} actions={<HelpButton topic="nodes.listeners" />} />
      {canEdit ? (
        <form className="space-y-4 p-4" onSubmit={(e) => void handleSubmit(askToSave)(e)} noValidate>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-[1fr_auto_auto]">
            <div className="space-y-2">
              <Label htmlFor="node-tls-domain-edit">{t('nodes.field_tls_domain')}</Label>
              <Input id="node-tls-domain-edit" className="mono" {...register('tls_domain')} aria-invalid={!!errors.tls_domain} />
              {errors.tls_domain && <p className="text-label text-destructive">{t('nodes.validation_hostname')}</p>}
            </div>
            <div className="space-y-2">
              <Label htmlFor="node-classic-port-edit">{t('nodes.field_classic_port')}</Label>
              <Input
                id="node-classic-port-edit"
                type="number"
                min={1024}
                max={65535}
                className="mono w-28"
                {...register('classic_port')}
                aria-invalid={!!errors.classic_port}
              />
              {errors.classic_port && <p className="text-label text-destructive">{t('nodes.validation_classic_port')}</p>}
            </div>
            <div className="space-y-2">
              <Label htmlFor="node-public-ip-edit">{t('nodes.field_public_ip')}</Label>
              <Input
                id="node-public-ip-edit"
                className="mono w-40"
                inputMode="decimal"
                placeholder="203.0.113.10"
                {...register('public_ip')}
                aria-invalid={!!errors.public_ip}
              />
              {errors.public_ip && <p className="text-label text-destructive">{t('nodes.validation_ipv4')}</p>}
            </div>
          </div>

          <p className="text-label text-mute">{t('nodes.field_public_ip_hint')}</p>

          <div className="space-y-2">
            <Label htmlFor="node-ad-tag-edit">{t('nodes.field_ad_tag')}</Label>
            <Input
              id="node-ad-tag-edit"
              className="mono"
              placeholder={t('nodes.field_ad_tag_placeholder')}
              {...register('ad_tag')}
              aria-invalid={!!errors.ad_tag}
            />
            {errors.ad_tag && <p className="text-label text-destructive">{t('nodes.validation_ad_tag')}</p>}
            <p className="text-label text-mute">{t('nodes.field_ad_tag_hint')}</p>
            <div className="flex flex-wrap items-center gap-2">
              <a
                href="https://t.me/MTProxybot"
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 text-label text-brand-ink underline-offset-3 hover:underline"
              >
                {t('nodes.field_ad_tag_open_bot')}
                <ExternalLink className="size-3" aria-hidden="true" />
              </a>
              {node.public_ip && (
                <CopyButton
                  value={`${node.public_ip}:${node.classic_port}`}
                  showLabel
                  label={t('nodes.field_ad_tag_copy_address', { address: `${node.public_ip}:${node.classic_port}` })}
                  className="h-6 px-2 text-label"
                />
              )}
              {registrationSecret.data && (
                <CopyButton
                  value={registrationSecret.data.secret}
                  showLabel
                  label={t('nodes.field_ad_tag_copy_secret')}
                  className="h-6 px-2 text-label"
                />
              )}
            </div>
          </div>

          <p className="flex items-start gap-2 text-label text-warn">
            <span className="mt-1 size-[7px] shrink-0 rounded-pill bg-warn" aria-hidden="true" />
            {t('nodes.listeners_restart_note')}
          </p>

          <div className="flex justify-end">
            <Button type="submit" size="sm" disabled={isSubmitting || !isDirty}>
              {t('common.save')}
            </Button>
          </div>
        </form>
      ) : (
        <dl className="grid grid-cols-1 gap-px bg-hairline sm:grid-cols-2">
          <div className="flex items-center justify-between gap-3 bg-card px-4 py-3">
            <dt className="truncate text-label text-mute">{t('nodes.field_tls_domain')}</dt>
            <dd className="mono shrink-0 text-mono text-foreground">{node.tls_domain || '—'}</dd>
          </div>
          <div className="flex items-center justify-between gap-3 bg-card px-4 py-3">
            <dt className="truncate text-label text-mute">{t('nodes.field_classic_port')}</dt>
            <dd className="mono shrink-0 text-mono text-foreground">{node.classic_port}</dd>
          </div>
          <div className="flex items-center justify-between gap-3 bg-card px-4 py-3">
            <dt className="truncate text-label text-mute">{t('nodes.field_public_ip')}</dt>
            <dd className="mono shrink-0 text-mono text-foreground">{node.public_ip || '—'}</dd>
          </div>
          <div className="flex items-center justify-between gap-3 bg-card px-4 py-3">
            <dt className="truncate text-label text-mute">{t('nodes.field_ad_tag')}</dt>
            <dd className="mono shrink-0 text-mono text-foreground">{node.ad_tag || '—'}</dd>
          </div>
        </dl>
      )}

      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open) setPending(null);
        }}
        title={t(pending?.kind === 'public_ip' ? 'nodes.public_ip_confirm_title' : 'nodes.listeners_confirm_title')}
        description={t(
          pending?.kind === 'public_ip' ? 'nodes.public_ip_confirm_description' : 'nodes.listeners_confirm_description',
        )}
        destructive={pending?.kind !== 'public_ip'}
        confirmLabel={t('common.save')}
        onConfirm={() => (pending ? onSubmit(pending.values) : undefined)}
      />
    </Panel>
  );
}
