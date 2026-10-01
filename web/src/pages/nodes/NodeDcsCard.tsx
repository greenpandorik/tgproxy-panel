import { useTranslation } from 'react-i18next';

import { ErrorState } from '@/components/common/ErrorState';
import { SubSection } from '@/components/common/SubSection';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { HelpButton } from '@/help';
import { formatCompactDuration, formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';

import { DC_TONE_DOT, DC_TONE_TEXT, dcLatencyOf, dcTone, routeTone } from './dcDisplay';
import { DASH } from './nodeDisplay';

import type { DcLatency, NodeEngine, NodeHealth } from '@/api/types';

export interface NodeDcsCardProps {
  engine: NodeEngine;
  /** The node cannot be reached: the panel says so rather than printing figures it cannot vouch for. */
  offline: boolean;
  /** The last health readout; undefined while it is still on its way. */
  health?: NodeHealth;
  /** The health fetch failed for a reason other than the node being offline. */
  error: boolean;
  onRetry: () => void;
}

/** Telegram has five DCs, so the wait is drawn as five rows and the panel does not change height when they land. */
const SKELETON_ROWS = 5;

function RouteLine({ health }: { health: NodeHealth }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const tone = routeTone({ healthy: health.upstream_healthy ?? true, fails: health.upstream_fails ?? 0 });
  const ok = health.connect_success_total ?? 0;
  const total = ok + (health.connect_fail_total ?? 0);
  const age = health.upstream_last_check_age_secs;

  const parts = [
    t('nodes.dcs_route_direct'),
    t('nodes.dcs_connections', { ok: formatNumber(ok, lang), total: formatNumber(total, lang) }),
    age === undefined ? null : t('nodes.dcs_checked', { ago: t('common.ago', { value: formatCompactDuration(age, lang) }) }),
  ].filter((p): p is string => p !== null);

  return (
    <p className="flex items-center gap-2 border-b border-hairline px-4 py-2.5" data-testid="dc-route" data-tone={tone}>
      <span className={cn('size-[7px] shrink-0 rounded-pill', DC_TONE_DOT[tone])} aria-hidden="true" />
      <span className={cn('text-label', DC_TONE_TEXT[tone])}>{parts.join(' · ')}</span>
    </p>
  );
}

function DcRow({ dc }: { dc: DcLatency }) {
  const { t, i18n } = useTranslation();
  const ms = dcLatencyOf(dc);
  const tone = dcTone(ms);
  return (
    <TableRow>
      <TableCell className="mono text-mono text-mute">DC{dc.dc}</TableCell>
      <TableCell className={cn('mono text-right text-mono', DC_TONE_TEXT[tone])} data-testid="dc-latency" data-tone={tone}>
        {ms === null ? DASH : `${formatNumber(Math.round(ms), i18n.language)} ${t('common.ms')}`}
      </TableCell>
      <TableCell className={cn('mono text-right text-mono', dc.ip_preference ? 'text-mute' : 'text-dim')}>
        {dc.ip_preference || DASH}
      </TableCell>
    </TableRow>
  );
}

/** The table before it arrives: the route line and five rows, drawn empty. */
function DcsSkeleton() {
  return (
    <div className="divide-y divide-hairline overflow-hidden rounded-control border border-hairline">
      <div className="flex h-9 items-center px-4">
        <Skeleton className="h-3 w-56" />
      </div>
      {Array.from({ length: SKELETON_ROWS }).map((_, i) => (
        <div key={i} className="flex h-11 items-center gap-4 px-3">
          <Skeleton className="h-3 w-10" />
          <Skeleton className="ml-auto h-3 w-14" />
          <Skeleton className="h-3 w-10" />
        </div>
      ))}
    </div>
  );
}

function Note({ children }: { children: string }) {
  return <p className="text-body text-mute">{children}</p>;
}

/** The route to Telegram and each datacenter's latency right now. */
export function NodeDcsCard({ engine, offline, health, error, onRetry }: NodeDcsCardProps) {
  const { t } = useTranslation();
  const telemt = engine === 'telemt';
  const available = health ? (health.dc_data_available ?? Array.isArray(health.dcs)) : false;
  const dcs = [...(health?.dcs ?? [])].sort((a, b) => a.dc - b.dc);

  return (
    <SubSection title={t('nodes.dcs_now')} actions={<HelpButton topic="nodes.dcs" />}>
      {!telemt ? (
        <Note>{t('nodes.dcs_unavailable_engine')}</Note>
      ) : offline ? (
        <Note>{t('nodes.offline_message')}</Note>
      ) : error ? (
        <ErrorState inset message={t('common.error_generic')} retryLabel={t('common.refresh')} onRetry={onRetry} />
      ) : !health ? (
        <DcsSkeleton />
      ) : !available ? (
        <Note>{t('nodes.dcs_unavailable')}</Note>
      ) : dcs.length === 0 ? (
        <Note>{t('nodes.dcs_empty')}</Note>
      ) : (
        <div className="overflow-hidden rounded-control border border-hairline">
          <RouteLine health={health} />
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-24">{t('nodes.dcs_column_dc')}</TableHead>
                <TableHead className="w-32 text-right">{t('nodes.dcs_column_latency')}</TableHead>
                <TableHead className="text-right">{t('nodes.dcs_column_ip')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {dcs.map((dc) => (
                <DcRow key={dc.dc} dc={dc} />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </SubSection>
  );
}
