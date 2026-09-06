import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { usePublicStatus } from '@/api/status';
import { useUpdateStatus } from '@/api/updates';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { formatStars } from '@/lib/format';
import { cn } from '@/lib/utils';

import type { ReactElement, ReactNode } from 'react';

/*
 * Topbar status chips: panel version (tinted with the brand hue when a newer
 * release exists), the GitHub mark with the star count, and nodes online.
 * All three are read-only glances - the version chip only becomes a link when
 * there is somewhere useful to go (the release page).
 */

const CHIP =
  'mono inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-hairline-strong px-2 text-xs text-dim transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none';

type NodesTone = 'ok' | 'warn' | 'err' | 'dim';

function nodesTone(online: number, total: number): NodesTone {
  if (total === 0) return 'dim';
  if (online === 0) return 'err';
  if (online < total) return 'warn';
  return 'ok';
}

const TONE_DOT: Record<NodesTone, string> = {
  ok: 'bg-ok',
  warn: 'bg-warn',
  err: 'bg-err',
  dim: 'bg-dim',
};

/** The GitHub octocat mark, 14px, drawn in currentColor so it dims and lifts with the chip text. */
function GitHubMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor" aria-hidden="true" className={className}>
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z" />
    </svg>
  );
}

function versionLabel(current: string): string {
  return current === 'dev' || current === '' ? 'dev' : `v${current}`;
}

/** Wraps a chip in the shared tooltip; the trigger *is* the chip so nothing extra lands in the tab order. */
function WithTooltip({ label, children }: { label: string; children: ReactElement }) {
  return (
    <Tooltip>
      <TooltipTrigger render={children} />
      <TooltipContent side="bottom">{label}</TooltipContent>
    </Tooltip>
  );
}

function VersionChip() {
  const { t } = useTranslation();
  const { data } = useUpdateStatus();
  if (!data) return null;

  const label = versionLabel(data.current);
  const hasUpdate = data.enabled && data.update_available && data.latest_url !== '';

  if (!hasUpdate) {
    return (
      <WithTooltip label={t('shell.version_tooltip')}>
        <span data-testid="version-chip" className={CHIP}>
          {label}
        </span>
      </WithTooltip>
    );
  }

  const hint = t('shell.update_available', { version: data.latest });
  return (
    <WithTooltip label={hint}>
      <a
        data-testid="version-chip"
        data-update="true"
        href={data.latest_url}
        target="_blank"
        rel="noreferrer"
        aria-label={hint}
        className={cn(CHIP, 'border-brand-primary/50 text-brand-primary hover:border-brand-primary hover:text-brand-primary')}
      >
        <span className="size-1.5 rounded-full bg-brand-primary" aria-hidden="true" />
        {label}
      </a>
    </WithTooltip>
  );
}

function GitHubChip() {
  const { t } = useTranslation();
  const { data } = useUpdateStatus();
  if (!data) return null;

  const stars = formatStars(data.stars);
  return (
    <WithTooltip label={t('shell.github_tooltip')}>
      <a
        data-testid="github-chip"
        href={data.repo_url}
        target="_blank"
        rel="noreferrer"
        aria-label={t('shell.github_tooltip')}
        className={CHIP}
      >
        <GitHubMark />
        {stars && <span>{stars}</span>}
      </a>
    </WithTooltip>
  );
}

function NodesChip() {
  const { t } = useTranslation();
  const { data } = usePublicStatus({ refetchInterval: 30_000 });
  if (!data) return null;

  const tone = nodesTone(data.nodes_online, data.nodes_total);
  return (
    <WithTooltip label={t('shell.nodes_tooltip')}>
      <Link data-testid="nodes-chip" data-tone={tone} to="/nodes" aria-label={t('shell.nodes_tooltip')} className={CHIP}>
        <span className={cn('size-1.5 rounded-full', TONE_DOT[tone])} aria-hidden="true" />
        {data.nodes_online}/{data.nodes_total}
      </Link>
    </WithTooltip>
  );
}

/** Desktop topbar row. Hidden below `md`; `StatusMenuRows` carries the same facts into the user menu there. */
export function StatusChips({ className }: { className?: string }) {
  return (
    <div className={cn('hidden items-center gap-1.5 md:flex', className)}>
      <VersionChip />
      <GitHubChip />
      <NodesChip />
    </div>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 px-1.5 py-1 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className="mono flex items-center gap-1.5 text-foreground">{children}</span>
    </div>
  );
}

/**
 * The same three facts as plain rows, for the user dropdown on narrow
 * screens where the topbar has no room for chips.
 */
export function StatusMenuRows() {
  const { t } = useTranslation();
  const { data: update } = useUpdateStatus();
  const { data: status } = usePublicStatus({ refetchInterval: 30_000 });

  const hasUpdate = !!update && update.enabled && update.update_available && update.latest_url !== '';
  const stars = update ? formatStars(update.stars) : '';
  const tone = status ? nodesTone(status.nodes_online, status.nodes_total) : 'dim';

  return (
    <div className="md:hidden">
      {update && (
        <Row label={t('shell.version_tooltip')}>
          <span>{versionLabel(update.current)}</span>
        </Row>
      )}
      {hasUpdate && (
        <a
          href={update.latest_url}
          target="_blank"
          rel="noreferrer"
          className="mono flex items-center gap-1.5 px-1.5 py-1 text-xs text-brand-primary"
        >
          <span className="size-1.5 rounded-full bg-brand-primary" aria-hidden="true" />
          {t('shell.update_available', { version: update.latest })}
        </a>
      )}
      {update && (
        <Row label={t('shell.github_tooltip')}>
          <a
            href={update.repo_url}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1.5 hover:text-foreground"
          >
            <GitHubMark />
            {stars && <span>{stars}</span>}
          </a>
        </Row>
      )}
      {status && (
        <Row label={t('shell.nodes_tooltip')}>
          <Link to="/nodes" data-tone={tone} className="inline-flex items-center gap-1.5">
            <span className={cn('size-1.5 rounded-full', TONE_DOT[tone])} aria-hidden="true" />
            {status.nodes_online}/{status.nodes_total}
          </Link>
        </Row>
      )}
    </div>
  );
}
