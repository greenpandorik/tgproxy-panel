import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useNodeProfiles } from '@/api/nodes';
import { DataTableSkeleton } from '@/components/common/DataTable';
import { PanelEmpty } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { CollapsibleSection } from '@/components/common/CollapsibleSection';
import { Panel } from '@/components/common/Panel';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { ApiError } from '@/lib/api';
import { formatBytes, formatDate } from '@/lib/format';
import { bpsToMbit } from '@/lib/units';
import { cn } from '@/lib/utils';

import { DASH } from './nodeDisplay';

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

function telemtLimitsSummary(limits: TelemtLimits | undefined, t: TFunction): string {
  if (!limits) return DASH;
  const parts: string[] = [];
  if (limits.data_quota_bytes) {
    const value = formatBytes(limits.data_quota_bytes);
    parts.push(
      limits.data_quota_period
        ? t(`nodes.limit_quota_${limits.data_quota_period}`, { value })
        : t('nodes.limit_quota', { value }),
    );
  }
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
            <TableCell className="mono text-mono text-mute">{p.name}</TableCell>
            <TableCell className="max-w-48 truncate text-foreground">{keyCell(p, t)}</TableCell>
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
function CompareResult({
  nodeId,
  online,
  engine,
  dbProfiles,
}: {
  nodeId: string;
  online: boolean;
  engine: NodeEngine;
  dbProfiles: Profile[];
}) {
  const { t } = useTranslation();
  const liveQuery = useNodeProfiles(nodeId, online);
  const liveOffline = !online || (liveQuery.error instanceof ApiError && liveQuery.error.code === 'node_offline');

  if (liveOffline) return <PanelEmpty>{t('nodes.offline_message')}</PanelEmpty>;
  if (liveQuery.isLoading) return <DataTableSkeleton columns={4} rows={3} />;
  if (liveQuery.isError) {
    return (
      <ErrorState
        inset
        message={t('common.error_generic')}
        retryLabel={t('common.refresh')}
        onRetry={() => void liveQuery.refetch()}
      />
    );
  }

  const liveProfiles = (liveQuery.data?.items ?? []) as LiveProfile[];
  const dbNames = new Set(dbProfiles.map((p) => p.name));
  const liveNames = new Set(liveProfiles.map((p) => p.name));
  const dbOnly = dbProfiles.filter((p) => !liveNames.has(p.name));
  const nodeOnly = liveProfiles.filter((p) => !dbNames.has(p.name));

  if (dbOnly.length === 0 && nodeOnly.length === 0) {
    return (
      <p className="flex items-center gap-2 px-5 py-4 text-body text-mute">
        <span className="size-[7px] shrink-0 rounded-pill bg-ok" aria-hidden="true" />
        {t('nodes.profiles_compare_in_sync')}
      </p>
    );
  }

  return (
    <div className="space-y-4 py-2">
      {dbOnly.length > 0 && (
        <div>
          <h4 className="px-5 pb-2 text-label font-medium text-err">
            {t('nodes.profiles_compare_db_only')} · {dbOnly.length}
          </h4>
          <ProfilesTable profiles={dbOnly} showSync={false} engine={engine} showKeyMeta />
        </div>
      )}
      {nodeOnly.length > 0 && (
        <div>
          <h4 className="px-5 pb-2 text-label font-medium text-err">
            {t('nodes.profiles_compare_node_only')} · {nodeOnly.length}
          </h4>
          <ProfilesTable profiles={nodeOnly} showSync={false} engine={engine} showKeyMeta={false} />
        </div>
      )}
    </div>
  );
}

/** Every user the panel keeps on this server, and a check against what the server itself has. */
export function NodeProfilesSection({ nodeId, online, engine }: { nodeId: string; online: boolean; engine: NodeEngine }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [comparing, setComparing] = useState(false);
  const dbQuery = useNodeProfiles(nodeId, false);
  const dbProfiles = dbQuery.data?.items ?? [];
  const divergent = dbProfiles.filter((p) => isDbProfile(p) && DIVERGENT.has(p.sync_state)).length;

  return (
    <Panel>
      <CollapsibleSection
        title={comparing ? t('nodes.profiles_compare_title') : t('nodes.profiles_title')}
        summary={dbQuery.data ? String(dbProfiles.length) : undefined}
        tone={divergent > 0 ? 'err' : 'neutral'}
        open={open}
        onOpenChange={setOpen}
        action={
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              setComparing(!comparing);
              setOpen(true);
            }}
          >
            {comparing ? t('nodes.profiles_title') : t('nodes.profiles_compare')}
          </Button>
        }
      >
        {dbQuery.isLoading ? (
          <DataTableSkeleton columns={6} rows={3} />
        ) : dbQuery.isError ? (
          <ErrorState
            inset
            message={t('common.error_generic')}
            retryLabel={t('common.refresh')}
            onRetry={() => void dbQuery.refetch()}
          />
        ) : comparing ? (
          <CompareResult nodeId={nodeId} online={online} engine={engine} dbProfiles={dbProfiles as Profile[]} />
        ) : dbProfiles.length === 0 ? (
          <PanelEmpty>{t('nodes.profiles_empty')}</PanelEmpty>
        ) : (
          <ProfilesTable profiles={dbProfiles} showSync engine={engine} showKeyMeta />
        )}
      </CollapsibleSection>
    </Panel>
  );
}
