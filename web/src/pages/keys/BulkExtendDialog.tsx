import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

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
          <DialogTitle>{t('keys.bulk_extend_title')}</DialogTitle>
          <DialogDescription>{t('keys.bulk_extend_description', { count })}</DialogDescription>
        </DialogHeader>

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
