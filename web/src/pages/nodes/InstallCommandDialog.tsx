import { useTranslation } from 'react-i18next';

import { Link } from 'react-router-dom';
import { useNode, useNodeHealth } from '@/api/nodes';
import { Button } from '@/components/ui/button';
import { WebDiagnosticsCard } from '@/components/web/WebDiagnosticsCard';
import { CopyButton } from '@/components/common/CopyButton';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { HelpButton } from '@/help';
import { formatDateTime } from '@/lib/format';

interface InstallCommandDialogProps {
  nodeId?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  command: string;
  expiresAt: string;
  /** Shown when the command was opened from the node page (a fresh token was just minted, invalidating any earlier one). */
  regenerated?: boolean;
}

const STEPS = ['nodes.install_step1', 'nodes.install_step2', 'nodes.install_step3'] as const;

// The one line that turns a bare server into a node.
export function InstallCommandDialog({ open, onOpenChange, command, expiresAt, regenerated, nodeId }: InstallCommandDialogProps) {
  const { t, i18n } = useTranslation();
  const node = useNode(open ? nodeId ?? '' : '');
  const health = useNodeHealth(nodeId ?? '', open && !!node.data?.online);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92dvh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <div className="flex items-center gap-1.5">
            <DialogTitle>{t('nodes.install_title')}</DialogTitle>
            <HelpButton topic="nodes.install" className="-my-1.5" />
          </div>
          <DialogDescription>{t('nodes.install_description')}</DialogDescription>
        </DialogHeader>

        <ol className="flex gap-4 border-b border-hairline pb-3 text-label">
          {[1, 2].map((step) => (
            <li key={step} className="flex items-center gap-1.5 text-ok">
              <span aria-hidden="true">✓</span>{step}. {t(`nodes.wizard_step_${step}`)}
            </li>
          ))}
          <li aria-current="step" className="font-semibold text-foreground">3. {t('nodes.wizard_step_3')}</li>
        </ol>

        <div>
          <div className="flex items-start gap-2 rounded-surface border border-hairline-strong bg-background p-3">
            <pre className="mono min-w-0 flex-1 overflow-x-auto text-mono break-all whitespace-pre-wrap text-foreground">
              {command}
            </pre>
            <CopyButton value={command} className="-mt-1 -mr-1 shrink-0" />
          </div>
          <p className="mono mt-2 text-mono text-mute">
            {t('nodes.install_expires', { time: formatDateTime(expiresAt, i18n.language) })}
          </p>
        </div>

        <ol className="space-y-2 border-t border-hairline pt-4">
          {STEPS.map((step, i) => (
            <li key={step} className="flex gap-2.5 text-body text-mute">
              {/* The counter takes the body size, not the mono one, so its
                  baseline sits on the step's first line. */}
              <span className="mono shrink-0 text-mute">{i + 1}</span>
              {t(step)}
            </li>
          ))}
        </ol>

        {nodeId && <div className="space-y-3 border-t border-hairline pt-4">
          <p role="status" className="font-medium">{t(node.data?.online ? 'nodes.install_connected' : 'nodes.install_waiting')}</p>
          {(node.isError || health.isError) && <p role="alert" className="text-destructive">{node.error?.message ?? health.error?.message}</p>}
          {health.data && <ul className="grid grid-cols-1 gap-2 text-label sm:grid-cols-2">{[['engine', health.data.relay_active], ['caddy', health.data.caddy_active], ['ready', health.data.readyz]].map(([name, ok]) => <li key={String(name)} className={ok ? 'text-ok' : 'text-warn'}>{ok ? '✓' : '○'} {t(`nodes.install_health_${name}`)}</li>)}</ul>}
          {node.data?.online && <><WebDiagnosticsCard nodeId={nodeId} /><Button nativeButton={false} render={<Link to={`/nodes/${nodeId}`} />}>{t('nodes.install_open')}</Button></>}
        </div>}
        {regenerated && (
          <p role="alert" className="flex items-start gap-2 text-label text-warn">
            <span className="mt-1 size-[7px] shrink-0 rounded-pill bg-warn" aria-hidden="true" />
            {t('nodes.install_regenerate_note')}
          </p>
        )}
      </DialogContent>
    </Dialog>
  );
}
