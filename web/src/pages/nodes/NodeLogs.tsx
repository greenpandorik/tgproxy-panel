import { Pause, Play, Terminal, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { nodeLogsUrl, useNode } from '@/api/nodes';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { ENTER_CLASS } from '@/components/ui/motion';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import { Switch } from '@/components/ui/switch';
import { cn } from '@/lib/utils';

import type { LogLine } from '@/api/types';

const SERVICES = [
  { id: 'tproxy-server', labelKey: 'nodes.logs_service_tproxy' },
  { id: 'mtproxy', labelKey: 'nodes.logs_service_mtproxy' },
  { id: 'caddy', labelKey: 'nodes.logs_service_caddy' },
] as const;

const LINE_OPTIONS = [100, 200, 500, 1000, 2000];
const MAX_BUFFER = 5000;

/**
 * A service filter, as a toggle chip rather than a checkbox and a word.
 * The three chips are the log's own vocabulary - unit names, in mono - and a
 * selected one reads as pressed: hairline goes strong, text goes to --fg.
 */
function ServiceChip({ label, selected, onToggle }: { label: string; selected: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={selected}
      onClick={onToggle}
      className={cn(
        'mono h-7 rounded-control border px-2 text-micro transition-[background-color,border-color,color,scale] outline-none active:scale-[0.985] focus-visible:ring-2 focus-visible:ring-ring/70',
        selected
          ? 'border-hairline-strong bg-elevated text-foreground'
          : 'border-hairline bg-transparent text-mute hover:text-foreground',
      )}
    >
      {label}
    </button>
  );
}

export function NodeLogs({ nodeId, online }: { nodeId: string; online: boolean }) {
  const { t } = useTranslation();
  // The `online` flag is only as fresh as the last node poll, so it is not used
  // to gate the stream: the tab always attempts the EventSource and shows the
  // error state if it fails. A failure also refetches the node, so a status that
  // has gone stale in either direction corrects itself immediately.
  const { refetch: refetchNode } = useNode(nodeId);
  const [services, setServices] = useState<string[]>(['tproxy-server']);
  const [lines, setLines] = useState(200);
  const [follow, setFollow] = useState(true);
  const [paused, setPaused] = useState(false);
  const [buffer, setBuffer] = useState<LogLine[]>([]);
  const [connected, setConnected] = useState(false);
  const [disconnected, setDisconnected] = useState(false);
  const [reconnectNonce, setReconnectNonce] = useState(0);

  const scrollRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    // Nothing to reset here on the early-return path: React already invoked the
    // previous run's cleanup (closing its EventSource) before this run started.
    if (services.length === 0) return undefined;

    const es = new EventSource(nodeLogsUrl(nodeId, services, lines, follow));

    es.onopen = () => {
      setConnected(true);
      setDisconnected(false);
    };
    es.addEventListener('log', (ev) => {
      try {
        const data = JSON.parse((ev as MessageEvent).data) as LogLine;
        setBuffer((prev) => {
          const next = [...prev, data];
          return next.length > MAX_BUFFER ? next.slice(next.length - MAX_BUFFER) : next;
        });
      } catch {
        // ignore malformed line
      }
    });
    es.addEventListener('end', () => {
      es.close();
      setConnected(false);
    });
    es.onerror = () => {
      setConnected(false);
      setDisconnected(true);
      es.close();
      // The stream is the freshest signal about reachability; pull the node row
      // so the rest of the page stops showing a status the stream just refuted.
      void refetchNode();
    };

    return () => {
      es.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- services array identity changes each toggle; join() below dedupes
  }, [nodeId, services.join(','), lines, follow, refetchNode, reconnectNonce]);

  useEffect(() => {
    if (paused) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [buffer, paused]);

  const toggleService = (id: string) => {
    setServices((prev) => (prev.includes(id) ? prev.filter((s) => s !== id) : [...prev, id]));
  };

  const visibleLines = useMemo(() => buffer, [buffer]);

  const streamState = disconnected ? 'offline' : connected ? 'online' : 'pending';

  return (
    <Panel className={ENTER_CLASS}>
      <PanelHeader
        icon={Terminal}
        title={t('nodes.logs_title')}
        meta={visibleLines.length > 0 ? String(visibleLines.length) : undefined}
        actions={
          <>
            <Button type="button" variant="ghost" size="sm" onClick={() => setPaused((p) => !p)}>
              {paused ? <Play /> : <Pause />}
              {paused ? t('nodes.logs_resume') : t('nodes.logs_pause')}
            </Button>
            <Button type="button" variant="ghost" size="sm" onClick={() => setBuffer([])}>
              <Trash2 />
              {t('nodes.logs_clear')}
            </Button>
          </>
        }
      />

      <div className="flex flex-wrap items-center gap-x-5 gap-y-3 border-b border-hairline px-4 py-3">
        <div className="flex flex-wrap items-center gap-1.5">
          {SERVICES.map((svc) => (
            <ServiceChip
              key={svc.id}
              label={t(svc.labelKey)}
              selected={services.includes(svc.id)}
              onToggle={() => toggleService(svc.id)}
            />
          ))}
        </div>

        <div className="flex items-center gap-2">
          <Label className="text-mute" htmlFor="log-lines">
            {t('nodes.logs_lines')}
          </Label>
          <Select value={String(lines)} onValueChange={(v) => v && setLines(Number(v))}>
            <SelectTrigger id="log-lines" size="sm" className="mono">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {LINE_OPTIONS.map((n) => (
                <SelectItem key={n} value={String(n)} className="mono">
                  {n}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <label className="flex items-center gap-2 text-body text-mute">
          <Switch checked={follow} onCheckedChange={setFollow} />
          {t('nodes.logs_follow')}
        </label>

        <span className="ml-auto inline-flex items-center gap-2 text-micro text-mute">
          <span
            className={cn(
              'size-[7px] shrink-0 rounded-pill',
              streamState === 'online' ? 'bg-ok' : streamState === 'offline' ? 'bg-err' : 'bg-pending',
            )}
            aria-hidden="true"
          />
          {t(`nodes.logs_stream_${streamState}`)}
        </span>
      </div>

      {services.length === 0 && <p className="px-4 pt-4 text-label text-err">{t('nodes.logs_select_service')}</p>}

      <div
        ref={scrollRef}
        className="mono m-4 h-96 overflow-y-auto rounded-surface border border-hairline bg-background p-3 text-mono text-mute"
      >
        {visibleLines.length === 0 ? (
          disconnected ? (
            <div className="flex flex-col items-start gap-3 font-sans text-body">
              <p className="text-mute">{online ? t('nodes.logs_disconnected') : t('nodes.offline_message')}</p>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  setDisconnected(false);
                  setReconnectNonce((n) => n + 1);
                }}
              >
                {t('common.refresh')}
              </Button>
            </div>
          ) : connected ? (
            <p className="font-sans text-body text-mute">{t('nodes.logs_empty')}</p>
          ) : (
            <div className="space-y-2">
              <p className="font-sans text-body text-mute">{t('nodes.logs_connecting')}</p>
              {['w-3/5', 'w-2/5', 'w-4/5', 'w-1/2'].map((w) => (
                <Skeleton key={w} className={cn('h-3', w)} />
              ))}
            </div>
          )
        ) : (
          visibleLines.map((l, i) => (
            <div key={i} className="break-all whitespace-pre-wrap">
              <span className="text-mute">[{l.service}]</span> {l.line}
            </div>
          ))
        )}
      </div>
    </Panel>
  );
}
