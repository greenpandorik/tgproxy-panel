import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useCreateSubscription, useRevokeSubscription } from '@/api/keys';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { CopyButton } from '@/components/common/CopyButton';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';

interface SubscriptionLinkSectionProps {
  keyId: string;
  subscriptionActive: boolean;
  isWriter: boolean;
  /** True for a revoked key: creating or rotating a link for it makes no sense. */
  locked?: boolean;
}

/**
 * "Subscription link" section shared by KeyLinkDialog and KeyDetailDrawer: create
 * or rotate a per-key subscription URL (POST /keys/{id}/subscription, which
 * returns the URL and its QR code once - the server only ever stores a hash, so
 * neither can be recovered later) and revoke it.
 *
 * Because the URL is shown exactly once, the moment it exists it becomes the
 * loudest thing in the section: a recessed mono field with copy beside it and
 * the QR on the same white tile the per-node links use. Everything before that
 * moment is a hairline row and a button.
 *
 * The freshly-issued URL is local, one-time UI state. Callers reusing this
 * component across different keys (the dialog/drawer stays mounted while its
 * `keyId` prop changes) must render it with `key={keyId}` so that state resets
 * instead of leaking the previous key's link.
 */
export function SubscriptionLinkSection({ keyId, subscriptionActive, isWriter, locked }: SubscriptionLinkSectionProps) {
  const { t } = useTranslation();
  const [created, setCreated] = useState<{ url: string; qrDataUri: string } | null>(null);
  const [rotateOpen, setRotateOpen] = useState(false);
  const createSub = useCreateSubscription(keyId);
  const revokeSub = useRevokeSubscription(keyId);

  const issue = async (successKey: string) => {
    try {
      const res = await createSub.mutateAsync();
      setCreated({ url: res.url, qrDataUri: res.qr_data_uri });
      toast.add({ description: t(successKey), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const handleRevoke = async () => {
    try {
      await revokeSub.mutateAsync();
      setCreated(null);
      toast.add({ description: t('keys.subscription_revoke_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <section className="space-y-2 border-t border-hairline pt-3">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-medium text-foreground">{t('keys.subscription_title')}</h3>
        {subscriptionActive && (
          <span className="mono inline-flex items-center gap-1.5 text-xs text-ok">
            <span className="size-[7px] shrink-0 rounded-full bg-ok" aria-hidden="true" />
            {t('keys.subscription_status_active')}
          </span>
        )}
      </div>
      <p className="text-xs text-mute">{t('keys.subscription_description')}</p>

      {created && (
        <div className="rounded-md border border-hairline-strong p-3">
          <p className="mono flex items-start gap-2 text-xs text-warn">
            <span className="mt-1 size-[7px] shrink-0 rounded-full bg-warn" aria-hidden="true" />
            {t('keys.subscription_shown_once')}
          </p>
          <div className="mt-2.5 flex gap-3">
            <img
              src={created.qrDataUri}
              alt={t('keys.subscription_qr_alt')}
              width={112}
              height={112}
              className="size-28 shrink-0 rounded-lg bg-white p-3"
            />
            <div className="flex min-w-0 flex-1 items-start gap-1.5">
              <Input
                readOnly
                value={created.url}
                className="mono h-7 text-xs"
                onFocus={(e) => e.target.select()}
                aria-label={t('keys.subscription_title')}
              />
              <CopyButton value={created.url} label={t('keys.subscription_copy')} className="shrink-0" />
            </div>
          </div>
        </div>
      )}

      {isWriter && !locked && (
        <div className="flex flex-wrap gap-2 pt-0.5">
          {!subscriptionActive ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={createSub.isPending}
              onClick={() => void issue('keys.subscription_create_success')}
            >
              {t('keys.subscription_create')}
            </Button>
          ) : (
            <>
              <Button type="button" variant="outline" size="sm" disabled={createSub.isPending} onClick={() => setRotateOpen(true)}>
                {t('keys.subscription_rotate')}
              </Button>
              <Button
                type="button"
                variant="destructive"
                size="sm"
                disabled={revokeSub.isPending}
                onClick={() => void handleRevoke()}
              >
                {t('keys.subscription_revoke')}
              </Button>
            </>
          )}
        </div>
      )}

      <ConfirmDialog
        open={rotateOpen}
        onOpenChange={setRotateOpen}
        title={t('keys.subscription_rotate_confirm_title')}
        description={t('keys.subscription_rotate_confirm_description')}
        confirmLabel={t('keys.subscription_rotate')}
        onConfirm={() => issue('keys.subscription_rotate_success')}
      />
    </section>
  );
}
