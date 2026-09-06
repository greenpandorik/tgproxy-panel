import { useTranslation } from 'react-i18next';

import { CopyButton } from '@/components/common/CopyButton';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { HelpButton } from '@/help';
import { formatDateTime } from '@/lib/format';

interface InstallCommandDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  command: string;
  expiresAt: string;
  /** Shown when the command was opened from the node page (a fresh token was just minted, invalidating any earlier one). */
  regenerated?: boolean;
}

const STEPS = ['nodes.install_step1', 'nodes.install_step2', 'nodes.install_step3'] as const;

/**
 * The one line that turns a bare server into a node.
 *
 * The command is the whole dialog, so it gets the only recessed surface here -
 * a mono block on the page ground with copy sitting inside it - and everything
 * else recedes: the expiry as a dim mono stamp, and the three things the target
 * server must already be true about, numbered because the operator works
 * through them in order before pasting anything.
 */
export function InstallCommandDialog({ open, onOpenChange, command, expiresAt, regenerated }: InstallCommandDialogProps) {
  const { t, i18n } = useTranslation();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <div className="flex items-center gap-1.5">
            <DialogTitle>{t('nodes.install_title')}</DialogTitle>
            <HelpButton topic="nodes.install" className="-my-1.5" />
          </div>
          <DialogDescription>{t('nodes.install_description')}</DialogDescription>
        </DialogHeader>

        <div>
          <div className="flex items-start gap-2 rounded-md border border-hairline-strong bg-background p-3">
            <pre className="mono min-w-0 flex-1 overflow-x-auto text-xs leading-relaxed break-all whitespace-pre-wrap text-foreground">
              {command}
            </pre>
            <CopyButton value={command} className="-mt-1 -mr-1 shrink-0" />
          </div>
          <p className="mono mt-2 text-xs text-dim">
            {t('nodes.install_expires', { time: formatDateTime(expiresAt, i18n.language) })}
          </p>
        </div>

        <ol className="space-y-1.5 border-t border-hairline pt-3">
          {STEPS.map((step, i) => (
            <li key={step} className="flex gap-2.5 text-sm text-mute">
              <span className="mono shrink-0 text-xs leading-[1.45] text-dim">{i + 1}</span>
              {t(step)}
            </li>
          ))}
        </ol>

        {regenerated && (
          <p role="alert" className="mono flex items-start gap-2 text-xs text-warn">
            <span className="mt-1 size-[7px] shrink-0 rounded-full bg-warn" aria-hidden="true" />
            {t('nodes.install_regenerate_note')}
          </p>
        )}
      </DialogContent>
    </Dialog>
  );
}
