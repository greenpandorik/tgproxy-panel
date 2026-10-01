import { ChevronRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { isMetricPresent } from '@/components/common/metric';
import { ServerOrderList } from '@/components/common/ServerOrder';
import { DragHandle } from '@/components/common/Sortable';
import { useSortableItem } from '@/components/common/sortableItem';
import { StatusBadge } from '@/components/common/StatusBadge';
import { Button } from '@/components/ui/button';
import { formatCompactAge, formatCompactDuration, formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';
import { DC_TONE_TEXT, dcTone, nodeDcLatency } from '@/pages/nodes/dcDisplay';
import { DASH, LOAD_TONE_CLASS, loadTone, nodeLoad, telemtUpdateTarget, telemtVersion } from '@/pages/nodes/nodeDisplay';

import { CERT_WARN_DAYS, formatDays } from './certDisplay';
import { daysLeft } from './fleetFacts';

import type { Node } from '@/api/types';
import type { ReactNode } from 'react';

function Row({ label, children, bar }: { label: string; children: ReactNode; bar?: { percent: number } }) {
  return (
    <div className="border-t border-hairline py-1.5">
      <div className="flex items-baseline justify-between gap-3">
        <dt className="text-label text-mute">{label}</dt>
        <dd className="mono text-right text-mono text-foreground">{children}</dd>
      </div>
      {bar && (
        <span className="mt-1.5 block h-1 overflow-hidden rounded-pill bg-hairline" aria-hidden="true">
          <span className={cn('block h-full', LOAD_TONE_CLASS[loadTone(bar.percent)].bar)} style={{ width: `${bar.percent}%` }} />
        </span>
      )}
    </div>
  );
}

function CertRow({ expiresAt }: { expiresAt?: string | null }) {
  const { t, i18n } = useTranslation();
  if (!expiresAt) return null;
  const days = daysLeft(expiresAt);
  const tone = days < 0 ? 'text-err' : days < CERT_WARN_DAYS ? 'text-warn' : '';
  return (
    <Row label={t('monitoring.card_cert')}>
      <span className={tone}>{days < 0 ? t('monitoring.cert_expired') : formatDays(days, i18n.language)}</span>
    </Row>
  );
}

function LiveRows({ node, certExpiresAt }: { node: Node; certExpiresAt?: string | null }) {
  const { t, i18n } = useTranslation();
  const num = (v: number) => formatNumber(v, i18n.language);
  const load = nodeLoad(node);
  const latency = nodeDcLatency(node);
  const waiting = node.dirty_since ? formatCompactAge(node.dirty_since, i18n.language) : null;
  return (
    <>
      <Row label={t('common.people_online')}>
        {isMetricPresent(node.people_online)
          ? t('common.approx', { value: num(node.people_online), context: node.people_online === 0 ? 'zero' : undefined })
          : DASH}
      </Row>
      {load?.cpu !== undefined ? (
        <Row label={t('monitoring.card_cpu')} bar={{ percent: load.cpu }}>
          {Math.round(load.cpu)}%
        </Row>
      ) : (
        <Row label={t('monitoring.card_cpu')}>{DASH}</Row>
      )}
      {load ? (
        <Row label={t('monitoring.card_ram')} bar={{ percent: load.mem }}>
          {Math.round(load.mem)}%
        </Row>
      ) : (
        <Row label={t('monitoring.card_ram')}>{DASH}</Row>
      )}
      {latency !== undefined && (
        <Row label={t('monitoring.card_telegram')}>
          <span className={DC_TONE_TEXT[dcTone(latency)]}>
            {num(Math.round(latency))} {t('common.ms')}
          </span>
        </Row>
      )}
      <CertRow expiresAt={certExpiresAt} />
      {node.health && node.health.uptime_seconds > 0 && (
        <Row label={t('monitoring.card_uptime')}>{formatCompactDuration(node.health.uptime_seconds, i18n.language)}</Row>
      )}
      {node.dirty && (
        <Row label={t('monitoring.card_changes')}>
          <span className="text-warn">
            {waiting ? t('monitoring.card_changes_wait', { value: waiting }) : t('monitoring.card_changes_waiting')}
          </span>
        </Row>
      )}
    </>
  );
}

function VersionRow({ node, pinned }: { node: Node; pinned?: string }) {
  const { t } = useTranslation();
  const current = node.engine === 'telemt' ? telemtVersion(node) : '';
  if (!current) return null;
  const next = telemtUpdateTarget(node, pinned);
  return (
    <Row label={t('monitoring.card_telemt')}>
      {next ? <span className="text-warn">{t('monitoring.card_telemt_update', { current, next })}</span> : current}
    </Row>
  );
}

function ServerStatusCard({
  node,
  certExpiresAt,
  pinned,
  writer,
}: {
  node: Node;
  certExpiresAt?: string | null;
  pinned?: string;
  writer: boolean;
}) {
  const { t, i18n } = useTranslation();
  const { setNode, style, dragging, handle } = useSortableItem(node.id, !writer);
  const { activator, attributes, listeners } = handle;
  const offline = node.status === 'offline';
  const age = formatCompactAge(node.last_seen_at, i18n.language);

  return (
    <li
      ref={setNode}
      style={style}
      className={cn(
        'flex min-w-0 flex-col rounded-surface border bg-card px-4 pt-2.5 pb-3',
        offline ? 'border-err/50' : 'border-hairline-strong',
        dragging && 'border-brand-primary/60 shadow-popover',
      )}
    >
      <div className="flex min-w-0 items-center gap-2 pb-2">
        {writer && (
          <DragHandle
            activator={activator}
            attributes={attributes}
            listeners={listeners}
            label={t('nodes.reorder_handle', { name: node.name })}
            className="-ml-2"
          />
        )}
        <StatusBadge status={node.status} hideLabel />
        <h3 className="min-w-0 flex-1 truncate text-body font-semibold text-foreground">{node.name}</h3>
        <Button variant="link" size="sm" className="-mr-2 px-2" nativeButton={false} render={<Link to={`/nodes/${node.id}`} />}>
          {t('monitoring.open_node')}
          <ChevronRight />
        </Button>
      </div>
      {node.status === 'pending' ? (
        <p className="border-t border-hairline pt-2 text-body text-mute">{t('nodes.last_seen_never')}</p>
      ) : offline ? (
        <>
          <p className="border-t border-hairline py-2 text-body text-err">
            {age ? t('monitoring.card_offline_for', { value: age }) : t('monitoring.card_offline')}
          </p>
          <dl>
            <Row label={t('common.people_online')}>{DASH}</Row>
            <CertRow expiresAt={certExpiresAt} />
            <VersionRow node={node} pinned={pinned} />
          </dl>
        </>
      ) : (
        <dl>
          <LiveRows node={node} certExpiresAt={certExpiresAt} />
          {telemtUpdateTarget(node, pinned) && <VersionRow node={node} pinned={pinned} />}
        </dl>
      )}
    </li>
  );
}

/** Every server as a card of its key numbers, in the shared order; a writer drags a card by its grip. */
export function ServerStatusCards({
  nodes,
  certs,
  pinned,
  writer,
}: {
  nodes: Node[];
  /** Certificate expiry by server id, from the monitoring overview. */
  certs: Map<string, string | null | undefined>;
  /** The telemt build the panel pins, when known. */
  pinned?: string;
  writer: boolean;
}) {
  return (
    <ServerOrderList servers={nodes} layout="grid">
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,280px),1fr))] gap-3">
        {nodes.map((node) => (
          <ServerStatusCard key={node.id} node={node} certExpiresAt={certs.get(node.id)} pinned={pinned} writer={writer} />
        ))}
      </ul>
    </ServerOrderList>
  );
}
