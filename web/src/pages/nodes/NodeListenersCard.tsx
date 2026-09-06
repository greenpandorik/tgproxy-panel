import { zodResolver } from '@hookform/resolvers/zod';
import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { usePatchNode } from '@/api/nodes';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
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

const schema = z
  .object({
    tls_domain: z.string().trim().toLowerCase(),
    classic_port: z.coerce.number().int(),
  })
  .superRefine((val, ctx) => {
    if (!HOSTNAME_RE.test(val.tls_domain) || !val.tls_domain.includes('.')) {
      ctx.addIssue({ code: 'custom', path: ['tls_domain'], message: 'hostname' });
    }
    if (!Number.isInteger(val.classic_port) || val.classic_port < 1024 || val.classic_port > 65535) {
      ctx.addIssue({ code: 'custom', path: ['classic_port'], message: 'range' });
    }
  });

type FormValues = z.infer<typeof schema>;

/**
 * The telemt node's Fake-TLS listener: the domain it masks behind and the port it
 * answers on.
 *
 * Both are baked into every Fake-TLS link the panel has already handed out - the
 * secret encodes the domain - so changing one is not a settings tweak, it is
 * reissuing the links. The note says so before the button, and the save itself
 * goes through a confirmation: this is the one control on the tab that can break
 * links people are already using.
 *
 * Read-only for viewers: the values still matter to whoever is reading the tab,
 * the controls do not.
 */
export function NodeListenersCard({ node, canEdit }: { node: Node; canEdit: boolean }) {
  const { t } = useTranslation();
  const patchNode = usePatchNode(node.id);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const {
    register,
    handleSubmit,
    getValues,
    reset,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { tls_domain: node.tls_domain, classic_port: node.classic_port },
  });

  useEffect(() => {
    reset({ tls_domain: node.tls_domain, classic_port: node.classic_port });
  }, [node.tls_domain, node.classic_port, reset]);

  // The form validates on submit and the confirmation opens only if it passed, so
  // the operator is never asked to confirm a change that cannot be saved.
  const askToSave = () => setConfirmOpen(true);

  const onSubmit = async (values: FormValues) => {
    try {
      // Both fields always travel together: the API takes each as an optional
      // patch, and sending only the one that changed makes the two forms of this
      // card behave differently for no reason the operator can see.
      await patchNode.mutateAsync({ tls_domain: values.tls_domain, classic_port: values.classic_port });
      reset(values);
      toast.add({ description: t('nodes.listeners_saved'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <Panel>
      <PanelHeader title={t('nodes.listeners_title')} actions={<HelpButton topic="nodes.listeners" />} />
      {canEdit ? (
        <form className="space-y-3 p-4" onSubmit={(e) => void handleSubmit(askToSave)(e)} noValidate>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_auto]">
            <div className="space-y-1.5">
              <Label htmlFor="node-tls-domain-edit">{t('nodes.field_tls_domain')}</Label>
              <Input id="node-tls-domain-edit" className="mono" {...register('tls_domain')} aria-invalid={!!errors.tls_domain} />
              {errors.tls_domain && <p className="text-xs text-destructive">{t('nodes.validation_hostname')}</p>}
            </div>
            <div className="space-y-1.5">
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
              {errors.classic_port && <p className="text-xs text-destructive">{t('nodes.validation_classic_port')}</p>}
            </div>
          </div>

          <p className="flex items-start gap-2 text-xs text-warn">
            <span className="mt-1 size-[7px] shrink-0 rounded-full bg-warn" aria-hidden="true" />
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
          <div className="flex items-center justify-between gap-3 bg-card px-4 py-2.5">
            <dt className="truncate text-xs text-mute">{t('nodes.field_tls_domain')}</dt>
            <dd className="mono shrink-0 text-xs text-foreground">{node.tls_domain || '—'}</dd>
          </div>
          <div className="flex items-center justify-between gap-3 bg-card px-4 py-2.5">
            <dt className="truncate text-xs text-mute">{t('nodes.field_classic_port')}</dt>
            <dd className="mono shrink-0 text-xs text-foreground">{node.classic_port}</dd>
          </div>
        </dl>
      )}

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('nodes.listeners_confirm_title')}
        description={t('nodes.listeners_confirm_description')}
        destructive
        confirmLabel={t('common.save')}
        onConfirm={() => onSubmit(getValues())}
      />
    </Panel>
  );
}
