import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { DraftBanner } from '@/components/common/DraftBanner';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { HelpButton } from '@/help';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { useDraft } from '@/lib/drafts';

const EMPTY_DRAFT = { expires_at: '' };

interface BulkExtendDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  count: number;
  onConfirm: (expiresAtIso: string) => void | Promise<void>;
}

/** Small date-time prompt backing the bulk "Extend" action (POST /keys/bulk, action=extend). */
export function BulkExtendDialog({ open, onOpenChange, count, onConfirm }: BulkExtendDialogProps) {
  const { t } = useTranslation();
  const [value, setValue] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  const draft = useDraft('keys-bulk-extend', { expires_at: value }, { initial: EMPTY_DRAFT, open });

  const resumeDraft = () => {
    if (!draft.draft) return;
    setValue(draft.draft.value.expires_at);
    setError(false);
    draft.dismiss();
  };

  const handleConfirm = async () => {
    if (!value) {
      setError(true);
      return;
    }
    const d = new Date(value);
    if (Number.isNaN(d.getTime()) || d.getTime() <= Date.now()) {
      setError(true);
      return;
    }
    setBusy(true);
    try {
      await onConfirm(d.toISOString());
      draft.clear();
      setValue('');
      onOpenChange(false);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setValue('');
          setError(false);
        }
        onOpenChange(next);
      }}
    >
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <div className="flex items-center gap-1.5">
            <DialogTitle>{t('keys.bulk_extend_title')}</DialogTitle>
            <HelpButton topic="keys.extend" className="-my-1.5" />
          </div>
          <DialogDescription>{t('keys.bulk_extend_description', { count })}</DialogDescription>
        </DialogHeader>

        {draft.draft && <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />}

        <div className="space-y-1.5">
          <Label htmlFor="bulk-extend-date">{t('keys.field_expires_at')}</Label>
          <Input
            id="bulk-extend-date"
            type="datetime-local"
            className="mono"
            autoFocus
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
              setError(false);
            }}
            aria-invalid={error}
          />
          {error && <p className="text-xs text-destructive">{t('keys.validation_expires_future')}</p>}
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            {t('common.cancel')}
          </Button>
          <Button type="button" onClick={() => void handleConfirm()} disabled={busy}>
            {t('keys.bulk_extend_submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
