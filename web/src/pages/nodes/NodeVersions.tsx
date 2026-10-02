import { ArrowUp } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { cn } from '@/lib/utils';

import { telemtUpdateTarget, telemtVersion } from './nodeDisplay';

import type { Node } from '@/api/types';

function minor(version: string): [number, number] | null {
  const m = /^v?(\d+)\.(\d+)/.exec(version.trim());
  return m ? [Number(m[1]), Number(m[2])] : null;
}

/** True when the agent is a minor release or more behind the panel; patch releases often leave the agent unchanged. */
export function agentBehind(agent: string, panel: string | undefined): boolean {
  if (!panel) return false;
  const a = minor(agent);
  const p = minor(panel);
  if (!a || !p) return false;
  return a[0] < p[0] || (a[0] === p[0] && a[1] < p[1]);
}

function Chip({ name, version, newer }: { name: string; version: string; newer?: string }) {
  const { t } = useTranslation();
  return (
    <span
      title={newer ? t('nodes.version_newer', { name, version: newer }) : undefined}
      className={cn(
        'mono inline-flex h-6 items-center gap-1 rounded-control border px-2 text-micro whitespace-nowrap',
        newer ? 'border-warn/55 bg-warn/8 text-warn' : 'border-hairline-strong text-mute',
      )}
    >
      {name}
      <span className={newer ? 'text-warn' : 'text-foreground'}>{version || '—'}</span>
      {newer && (
        <>
          <ArrowUp size={12} strokeWidth={2} aria-hidden="true" />
          <span className="sr-only">{t('nodes.version_newer', { name, version: newer })}</span>
        </>
      )}
    </span>
  );
}

/** The proxy and agent builds a server runs, with the ones that have a newer release marked. */
export function NodeVersions({
  node,
  pinnedTelemt,
  panelVersion,
}: {
  node: Pick<Node, 'engine' | 'telemt_version' | 'tproxy_version' | 'telemt_update_available' | 'agent_version'>;
  pinnedTelemt?: string;
  panelVersion?: string;
}) {
  const { t } = useTranslation();
  const telemt = node.engine === 'telemt';
  const proxyVersion = telemt ? telemtVersion(node) : node.tproxy_version;
  const agent = node.agent_version?.trim() ?? '';
  return (
    <span className="flex flex-wrap gap-1.5">
      <Chip
        name={telemt ? 'telemt' : 'tproxy'}
        version={proxyVersion}
        newer={telemt ? telemtUpdateTarget(node, pinnedTelemt) || undefined : undefined}
      />
      <Chip
        name={t('nodes.version_agent')}
        version={agent}
        newer={agent && agentBehind(agent, panelVersion) ? panelVersion : undefined}
      />
    </span>
  );
}
