import { ChevronRight, Download } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { CopyButton } from '@/components/common/CopyButton';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { HelpButton } from '@/help';

import type { AccessKey } from '@/api/types';

interface BatchResultDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  keys: AccessKey[];
  onOpen: (keyId: string) => void;
}

/** Builds the "download all links" .txt blob: each user's subscription link, then every direct link, tab-separated. */
function buildLinksText(keys: AccessKey[]): string {
  const lines: string[] = [];
  for (const key of keys) {
    if (key.subscription_url) lines.push(`${key.label}\t${key.subscription_url}`);
  }
  lines.push('');
  for (const key of keys) {
    for (const link of key.links ?? []) {
      lines.push(`${key.label}\t${link.hostname}\t${link.tme}\t${link.tg}`);
    }
  }
  return lines.join('\n') + '\n';
}

/** Result list after a batch create: each user's subscription link to copy, plus a .txt export of every link. */
export function BatchResultDialog({ open, onOpenChange, keys, onOpen }: BatchResultDialogProps) {
  const { t } = useTranslation();

  const handleDownloadAll = () => {
    const blob = new Blob([buildLinksText(keys)], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'user-links.txt';
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <DialogTitle>{t('keys.batch_result_title', { count: keys.length })}</DialogTitle>
            <HelpButton topic="keys.batch" className="-my-1" />
          </div>
          <DialogDescription>{t('keys.batch_result_description')}</DialogDescription>
        </DialogHeader>

        <ul className="max-h-64 divide-y divide-hairline overflow-y-auto rounded-control border border-hairline">
          {keys.map((key) => (
            <li key={key.id} className="flex h-11 items-center justify-between gap-2 pr-1 pl-3">
              <span className="truncate text-body text-foreground">{key.label}</span>
              <span className="flex shrink-0 items-center gap-1">
                {key.subscription_url && (
                  <CopyButton value={key.subscription_url} label={t('keys.subscription_copy')} className="size-8" />
                )}
                <Button type="button" variant="ghost" size="sm" onClick={() => onOpen(key.id)}>
                  {t('users.open_user')}
                  <ChevronRight />
                </Button>
              </span>
            </li>
          ))}
        </ul>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={handleDownloadAll}>
            <Download />
            {t('keys.batch_download_all')}
          </Button>
          <Button type="button" onClick={() => onOpenChange(false)}>
            {t('common.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
