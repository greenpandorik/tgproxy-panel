import { Layers } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useNodeProfiles } from '@/api/nodes';
import { DataTableSkeleton } from '@/components/common/DataTable';
import { PanelEmpty } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { ENTER_CLASS } from '@/components/ui/motion';
import { ApiError } from '@/lib/api';
import { formatBytes, formatDate } from '@/lib/format';
import { bpsToMbit } from '@/lib/units';
import { cn } from '@/lib/utils';

import { DASH } from './nodeDisplay';

import type { ReactNode } from 'react';
import type { LiveProfile, NodeEngine, Profile, TelemtLimits } from '@/api/types';
import type { TFunction } from 'i18next';

/** Sync states that mean the panel and the node disagree about this profile. */
const DIVERGENT = new Set(['db_only', 'node_only']);

function limitsSummary(limits: Profile['limits'] | LiveProfile['limits'], t: TFunction): string {
  const parts: string[] = [];
  if (limits.max_sessions) parts.push(t('nodes.limit_sessions', { count: limits.max_sessions }));
  if (limits.max_streams) parts.push(t('nodes.limit_streams', { count: limits.max_streams }));
  if (limits.new_sessions_per_minute) parts.push(t('nodes.limit_per_minute', { count: limits.new_sessions_per_minute }));
  return parts.length > 0 ? parts.join(' · ') : DASH;
}

/**
 * The limits telemt is enforcing for this profile's key, in one mono line.
 *
 * Written in the units the operator set them in - gigabytes, megabits per second -
 * with the machine's own comparison sign, so a column of these can be scanned for
 * the profile whose ceiling differs from its neighbours. A key with no limits gets
 * an em dash, not "0": zero here means "not set", and the two read differently.
 */
function telemtLimitsSummary(limits: TelemtLimits | undefined, t: TFunction): string {
  if (!limits) return DASH;
  const parts: string[] = [];
  if (limits.data_quota_bytes) parts.push(t('nodes.limit_quota', { value: formatBytes(limits.data_quota_bytes) }));
  if (limits.rate_limit_up_bps) parts.push(t('nodes.limit_rate_up', { value: bpsToMbit(limits.rate_limit_up_bps) }));
  if (limits.rate_limit_down_bps) parts.push(t('nodes.limit_rate_down', { value: bpsToMbit(limits.rate_limit_down_bps) }));
  if (limits.max_unique_ips) parts.push(t('nodes.limit_ips', { count: limits.max_unique_ips }));
  if (limits.max_tcp_conns) parts.push(t('nodes.limit_conns', { count: limits.max_tcp_conns }));
  return parts.length > 0 ? parts.join(' · ') : DASH;
}

function isDbProfile(p: Profile | LiveProfile): p is Profile {
  return 'id' in p;
}

/** The key a profile serves, by the name the operator gave it. */
function keyCell(p: Profile | LiveProfile, t: TFunction): string {
  if (!isDbProfile(p) || !p.access_key_id) return t('nodes.profiles_key_default');
  return p.key_label || `#${p.access_key_id.slice(0, 8)}`;
}

/**
 * The profiles the relay is configured with. Every column but the name is a
 * machine value, so the whole table is mono: names on the left in the reading
 * face, everything the relay produced on the right in the typing face.
 */
function ProfilesTable({
  profiles,
  showSync,
  engine,
  showKeyMeta,
}: {
  profiles: (Profile | LiveProfile)[];
  showSync: boolean;
  /** telemt profiles are limited per key, tproxy profiles per relay session. */
  engine: NodeEngine;
  /** DB rows carry the key's expiry; the live listing read off the node does not. */
  showKeyMeta: boolean;
}) {
  const { t, i18n } = useTranslation();
  const telemt = engine === 'telemt';

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('nodes.profiles_column_name')}</TableHead>
          <TableHead>{t('nodes.profiles_column_key')}</TableHead>
          <TableHead>{t('nodes.profiles_column_carrier')}</TableHead>
          <TableHead>{t('nodes.profiles_column_limits')}</TableHead>
          {showKeyMeta && <TableHead className="text-right">{t('nodes.profiles_column_expires')}</TableHead>}
          {showSync && <TableHead className="text-right">{t('nodes.profiles_column_sync')}</TableHead>}
        </TableRow>
      </TableHeader>
      <TableBody>
        {profiles.map((p, i) => (
          <TableRow key={isDbProfile(p) ? p.id : `${p.name}-${i}`}>
            <TableCell className="font-medium text-foreground">{p.name}</TableCell>
            <TableCell className="max-w-40 truncate text-mute">{keyCell(p, t)}</TableCell>
            <TableCell className="mono text-mono text-mute">{p.carrier_mode}</TableCell>
            <TableCell className="mono max-w-72 text-mono whitespace-normal text-mute">
              {telemt ? telemtLimitsSummary(isDbProfile(p) ? p.telemt_limits : undefined, t) : limitsSummary(p.limits, t)}
            </TableCell>
            {showKeyMeta && (
              <TableCell className="mono text-right text-mono text-mute">
                {isDbProfile(p) && p.key_expires_at ? formatDate(p.key_expires_at, i18n.language) : DASH}
              </TableCell>
            )}
            {showSync && (
              <TableCell className="text-right">
                {isDbProfile(p) && (
                  <span
                    className={cn(
                      'text-label',
                      DIVERGENT.has(p.sync_state) ? 'text-err' : p.sync_state === 'pending' ? 'text-warn' : 'text-mute',
                    )}
                  >
                    {t(`nodes.sync_${p.sync_state}`, p.sync_state)}
                  </span>
                )}
              </TableCell>
            )}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

/** The request failed: what happened, and the button that tries again. */
function PanelError({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation();
  return <ErrorState inset message={t('common.error_generic')} retryLabel={t('common.refresh')} onRetry={onRetry} />;
}

function Section({ title, meta, actions, children }: { title: string; meta?: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <Panel className={ENTER_CLASS}>
      <PanelHeader icon={Layers} title={title} meta={meta} actions={actions} />
      {children}
    </Panel>
  );
}

export function NodeProfilesTab({ nodeId, online, engine }: { nodeId: string; online: boolean; engine: NodeEngine }) {
  const { t } = useTranslation();
  const [comparing, setComparing] = useState(false);

  const dbQuery = useNodeProfiles(nodeId, false);
  const liveQuery = useNodeProfiles(nodeId, comparing && online);

  const dbProfiles = dbQuery.data?.items ?? [];
  const liveOffline = comparing && (!online || (liveQuery.error instanceof ApiError && liveQuery.error.code === 'node_offline'));

  if (dbQuery.isLoading) return <DataTableSkeleton columns={6} rows={3} />;

  if (dbQuery.isError) {
    return (
      <Section title={t('nodes.profiles_title')}>
        <PanelError onRetry={() => void dbQuery.refetch()} />
      </Section>
    );
  }

  if (!comparing) {
    return (
      <Section
        title={t('nodes.profiles_title')}
        meta={dbProfiles.length > 0 ? String(dbProfiles.length) : undefined}
        actions={
          <Button type="button" variant="outline" size="sm" onClick={() => setComparing(true)}>
            {t('nodes.profiles_compare')}
          </Button>
        }
      >
        {dbProfiles.length === 0 ? (
          <PanelEmpty>{t('nodes.profiles_empty')}</PanelEmpty>
        ) : (
          <ProfilesTable profiles={dbProfiles} showSync engine={engine} showKeyMeta />
        )}
      </Section>
    );
  }

  const liveProfiles = (liveQuery.data?.items ?? []) as LiveProfile[];
  const dbNames = new Set(dbProfiles.map((p) => p.name));
  const liveNames = new Set(liveProfiles.map((p) => p.name));
  const dbOnly = dbProfiles.filter((p) => !liveNames.has(p.name));
  const nodeOnly = liveProfiles.filter((p) => !dbNames.has(p.name));

  const back = (
    <Button type="button" variant="outline" size="sm" onClick={() => setComparing(false)}>
      {t('nodes.profiles_title')}
    </Button>
  );

  if (liveOffline || liveQuery.isLoading || (dbOnly.length === 0 && nodeOnly.length === 0)) {
    return (
      <Section title={t('nodes.profiles_compare_title')} actions={back}>
        {liveOffline ? (
          <PanelEmpty>{t('nodes.offline_message')}</PanelEmpty>
        ) : liveQuery.isLoading ? (
          <DataTableSkeleton columns={4} rows={3} />
        ) : liveQuery.isError ? (
          <PanelError onRetry={() => void liveQuery.refetch()} />
        ) : (
          <p className="flex items-center justify-center gap-2 px-6 py-10 text-body text-mute">
            <span className="size-[7px] shrink-0 rounded-pill bg-ok" aria-hidden="true" />
            {t('nodes.profiles_compare_in_sync')}
          </p>
        )}
      </Section>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {dbOnly.length > 0 && (
        <Section title={t('nodes.profiles_compare_db_only')} meta={String(dbOnly.length)} actions={back}>
          <ProfilesTable profiles={dbOnly} showSync={false} engine={engine} showKeyMeta />
        </Section>
      )}
      {nodeOnly.length > 0 && (
        <Section
          title={t('nodes.profiles_compare_node_only')}
          meta={String(nodeOnly.length)}
          actions={dbOnly.length === 0 ? back : undefined}
        >
          <ProfilesTable profiles={nodeOnly} showSync={false} engine={engine} showKeyMeta={false} />
        </Section>
      )}
    </div>
  );
}
