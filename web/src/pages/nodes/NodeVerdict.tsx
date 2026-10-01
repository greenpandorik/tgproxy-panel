import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { CopyButton } from '@/components/common/CopyButton';
import { Panel } from '@/components/common/Panel';
import { TONE_VAR } from '@/components/common/statTone';
import { Button } from '@/components/ui/button';

import type { Verdict } from './serverVerdict';

const AGENT_COMMANDS = ['systemctl status tgwp-agent', 'journalctl -u tgwp-agent -n 100 --no-pager'];

/** What is wrong with the server, at the top of its page; nothing at all while it is fine. */
export function NodeVerdict({ verdict, canInstall = true }: { verdict: Verdict; canInstall?: boolean }) {
  const { t } = useTranslation();
  if (verdict.tone === 'ok') return null;
  const offline = verdict.tone === 'err';

  return (
    <div data-testid="node-verdict" data-tone={verdict.tone}>
      <Panel>
        <div className="flex flex-col gap-3 px-(--panel-x) py-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="flex min-w-0 items-start gap-3">
            <span
              className="mt-[9px] size-2 shrink-0 rounded-pill"
              style={{ background: TONE_VAR[verdict.tone] }}
              aria-hidden="true"
            />
            <div className="min-w-0">
              <p className="text-title text-foreground">{verdict.headline}</p>
              {verdict.lines.length > 0 && (
                <ul className="mt-1 space-y-0.5 text-body text-mute">
                  {verdict.lines.map((line) => (
                    <li key={line}>{line}</li>
                  ))}
                </ul>
              )}
            </div>
          </div>
          {offline && canInstall && (
            <Button
              variant="outline"
              size="sm"
              className="shrink-0"
              nativeButton={false}
              render={<Link to="?section=settings" />}
            >
              {t('nodes.verdict_open_install')}
            </Button>
          )}
        </div>
        {offline && (
          <div className="space-y-2 border-t border-hairline px-(--panel-x) py-3">
            <p className="text-label text-mute">{t('nodes.verdict_offline_commands')}</p>
            <div className="grid gap-1.5 lg:grid-cols-2">
              {AGENT_COMMANDS.map((cmd) => (
                <div key={cmd} className="flex min-w-0 items-center gap-1.5">
                  <code className="mono min-w-0 flex-1 break-all rounded-control sm:truncate sm:break-normal border border-hairline bg-[var(--field-bg)] px-2.5 py-1 text-mono text-foreground">
                    {cmd}
                  </code>
                  <CopyButton value={cmd} label={t('nodes.verdict_copy_command')} className="size-8 shrink-0" />
                </div>
              ))}
            </div>
            <p className="text-label text-mute">{t('nodes.verdict_offline_reinstall')}</p>
          </div>
        )}
      </Panel>
    </div>
  );
}
