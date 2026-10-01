import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Search } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { fleetKeys, useFleetRollouts } from '@/api/nodes';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/ui/toast';
import { api, ApiError } from '@/lib/api';
import { cn } from '@/lib/utils';

import { telemtUpdateTarget, telemtVersion } from './nodeDisplay';

import type { FleetRollout } from '@/api/nodes';
import type { Node } from '@/api/types';

/** Why a server cannot go into this rollout, or null when it can. */
function blocked(node: Node, pinned: string | undefined): 'offline' | 'current' | null {
  if (!node.online) return 'offline';
  if (pinned && !telemtUpdateTarget(node, pinned)) return 'current';
  return null;
}

/** The servers a rollout would touch, in the shared order, matching the search. */
function matches(node: Node, query: string): boolean {
  const q = query.trim().toLowerCase();
  return !q || node.name.toLowerCase().includes(q) || node.hostname.toLowerCase().includes(q);
}

/** A running rollout's progress, with the button that stops the servers it has not started. */
export function FleetRolloutStatus({ rollout, nodes, writer }: { rollout: FleetRollout; nodes: Node[]; writer: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const stop = useMutation({
    mutationFn: () => api.post(`/api/v1/fleet/updates/${rollout.id}/stop`),
    onSettled: () => void qc.invalidateQueries({ queryKey: fleetKeys.rollouts }),
  });
  const current = nodes.find((n) => n.id === rollout.node_ids[rollout.cursor]);
  return (
    <div
      role="status"
      className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-surface border border-hairline-strong bg-card px-(--panel-x) py-3"
    >
      <p className="min-w-0 flex-1 text-body text-foreground">
        {t('nodes.fleet_progress', {
          status: t(`fleet.${rollout.status}`),
          done: Math.min(rollout.cursor, rollout.node_ids.length),
          total: rollout.node_ids.length,
        })}
        {current && rollout.status === 'running' && <span className="text-mute"> · {current.name}</span>}
      </p>
      {rollout.error && <p className="mono w-full text-mono text-err">{rollout.error}</p>}
      {writer && rollout.status === 'running' && (
        <Button type="button" variant="outline" size="sm" disabled={stop.isPending} onClick={() => stop.mutate()}>
          {t('fleet.stop')}
        </Button>
      )}
    </div>
  );
}

function FleetServerList({
  servers,
  chosen,
  pinned,
  onToggle,
}: {
  servers: Node[];
  chosen: Node[];
  pinned?: string;
  onToggle: (id: string, on: boolean) => void;
}) {
  const { t } = useTranslation();
  if (servers.length === 0) {
    return (
      <p className="rounded-surface border border-hairline px-4 py-6 text-center text-body text-mute">
        {t('nodes.fleet_none_found')}
      </p>
    );
  }
  return (
    <ul
      aria-label={t('nodes.fleet_list')}
      className="max-h-[min(50dvh,22rem)] min-h-24 divide-y divide-hairline overflow-y-auto overscroll-contain rounded-surface border border-hairline"
    >
      {servers.map((node) => {
        const reason = blocked(node, pinned);
        const position = chosen.findIndex((n) => n.id === node.id);
        const current = telemtVersion(node);
        const next = telemtUpdateTarget(node, pinned);
        return (
          <li key={node.id}>
            <label
              data-testid="fleet-server"
              className={cn(
                'grid grid-cols-[auto_1.5rem_minmax(0,1fr)_auto] items-center gap-3 px-3 py-2.5 text-body',
                reason ? 'text-mute' : 'cursor-pointer hover:bg-elevated',
              )}
            >
              <Checkbox checked={position >= 0} disabled={!!reason} onCheckedChange={(v) => onToggle(node.id, !!v)} />
              <span className="mono text-right text-mono text-mute">{position >= 0 ? position + 1 : ''}</span>
              <span className="min-w-0 truncate text-foreground">{node.name}</span>
              <span className={cn('mono text-mono', next && !reason ? 'text-foreground' : 'text-mute')}>
                {reason === 'offline'
                  ? t('nodes.fleet_offline')
                  : next
                    ? t('nodes.fleet_from_to', { from: current || '?', to: next })
                    : t('nodes.fleet_already', { version: current || pinned })}
              </span>
            </label>
          </li>
        );
      })}
    </ul>
  );
}

/** Choose servers for a telemt rollout: search, pick the outdated ones, and start them in list order. */
export function FleetUpdateDialog({
  open,
  onOpenChange,
  nodes,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  nodes: Node[];
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const rollouts = useFleetRollouts();
  const pinned = rollouts.data?.version;
  const running = rollouts.data?.items.find((r) => r.status === 'running');
  const servers = nodes.filter((n) => n.engine === 'telemt');
  const [query, setQuery] = useState('');
  const [picked, setPicked] = useState<Set<string>>(() => new Set());

  const chosen = servers.filter((n) => picked.has(n.id) && !blocked(n, pinned));
  const shown = servers.filter((n) => matches(n, query));
  const outdated = servers.filter((n) => !blocked(n, pinned) && telemtUpdateTarget(n, pinned));

  const start = useMutation({
    mutationFn: (ids: string[]) => api.post('/api/v1/fleet/updates', { node_ids: ids }),
    onSuccess: () => {
      toast.add({ description: t('nodes.fleet_started', { count: chosen.length }), type: 'success' });
      setPicked(new Set());
      onOpenChange(false);
    },
    onSettled: () => void qc.invalidateQueries({ queryKey: fleetKeys.rollouts }),
  });

  const toggle = (id: string, on: boolean) =>
    setPicked((current) => {
      const next = new Set(current);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[calc(100dvh-2rem)] flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{pinned ? t('nodes.fleet_title_version', { version: pinned }) : t('fleet.title')}</DialogTitle>
          <DialogDescription>{t('nodes.fleet_how')}</DialogDescription>
        </DialogHeader>
        {running && <FleetRolloutStatus rollout={running} nodes={nodes} writer />}
        <div className="flex flex-wrap gap-2">
          <label className="relative min-w-48 flex-1">
            <span className="sr-only">{t('nodes.fleet_search')}</span>
            <Search
              className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-mute"
              aria-hidden="true"
            />
            <Input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('nodes.fleet_search')}
              className="h-(--control-height) pl-8"
            />
          </label>
          <Button
            type="button"
            variant="outline"
            disabled={outdated.length === 0}
            onClick={() => setPicked(new Set(outdated.map((n) => n.id)))}
          >
            {t('nodes.fleet_select_outdated')}
          </Button>
        </div>
        <FleetServerList servers={shown} chosen={chosen} pinned={pinned} onToggle={toggle} />
        {start.error && (
          <p role="alert" className="text-body text-err">
            {start.error instanceof ApiError ? start.error.message : t('common.error_generic')}
          </p>
        )}
        <DialogFooter className="sm:items-center">
          <p className="mono text-mono text-mute sm:mr-auto">
            {t('nodes.fleet_selected', { count: chosen.length, total: servers.length })}
          </p>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button
            type="button"
            disabled={chosen.length === 0 || !!running || start.isPending}
            onClick={() => start.mutate(chosen.map((n) => n.id))}
          >
            {t('nodes.fleet_start', { count: chosen.length })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
