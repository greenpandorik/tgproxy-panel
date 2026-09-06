import { RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { useNodeStats } from '@/api/nodes';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ApiError } from '@/lib/api';
import { cn } from '@/lib/utils';

/**
 * MTProxy's own counters, printed as it reports them: raw key on the left,
 * raw value on the right, both mono, split into two columns so a couple of
 * dozen counters fit on one screen. No translation of the keys - these are the
 * names that appear in MTProxy's stats output and in every issue report about
 * it, and renaming them here would only make the two harder to match up.
 */
export function NodeStatsTab({ nodeId, online }: { nodeId: string; online: boolean }) {
  const { t } = useTranslation();
  const statsQuery = useNodeStats(nodeId, online);

  const offline = !online || (statsQuery.error instanceof ApiError && statsQuery.error.code === 'node_offline');
  const entries = statsQuery.data ? Object.entries(statsQuery.data) : [];

  return (
    <Panel>
      <PanelHeader
        title={t('nodes.stats_title')}
        meta={entries.length > 0 ? String(entries.length) : undefined}
        actions={
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => void statsQuery.refetch()}
            disabled={offline || statsQuery.isFetching}
          >
            <RefreshCw className={cn(statsQuery.isFetching && 'animate-spin')} />
            {t('common.refresh')}
          </Button>
        }
      />

      {offline ? (
        <p className="px-4 py-6 text-center text-sm text-mute">{t('nodes.offline_message')}</p>
      ) : statsQuery.isLoading ? (
        <div className="space-y-2 p-4">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      ) : entries.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-mute">{t('nodes.stats_empty')}</p>
      ) : (
        <dl className="grid grid-cols-1 gap-px bg-hairline lg:grid-cols-2">
          {entries.map(([key, value]) => (
            <div key={key} className="flex items-baseline justify-between gap-4 bg-card px-4 py-2">
              <dt className="mono truncate text-xs text-dim">{key}</dt>
              <dd className="mono shrink-0 text-xs text-foreground">{value}</dd>
            </div>
          ))}
          {/* An odd counter count would leave the grid's last slot empty, and the
              1px gaps are painted by the container - so the hole would show up as
              a lit rectangle. Fill it with the panel surface instead. */}
          {entries.length % 2 === 1 && <div className="hidden bg-card lg:block" aria-hidden="true" />}
        </dl>
      )}
    </Panel>
  );
}
