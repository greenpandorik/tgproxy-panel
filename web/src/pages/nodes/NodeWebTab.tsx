import { Gauge, Globe, Radar } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { useNodeHealth } from '@/api/nodes';
import { useNodeWebCarriers } from '@/api/web';
import { ErrorState } from '@/components/common/ErrorState';
import { MetricValue } from '@/components/common/MetricValue';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { StatusIndicator } from '@/components/common/StatusIndicator';
import { WebLifecycleControls } from '@/components/web/WebLifecycleControls';
import { WebPolicyCard } from '@/components/web/WebPolicyCard';
import { CapabilityNotice } from '@/components/web/CapabilityNotice';
import { CarrierDistribution } from '@/components/web/CarrierDistribution';
import { WebCounters } from '@/components/web/WebCounters';
import { WebDiagnosticsCard } from '@/components/web/WebDiagnosticsCard';
import { CAP_CARRIER_LEARNING, CAP_CARRIER_NEGOTIATION, CAP_WEB, nodeCapability } from '@/components/web/capability';
import { webRuntimeStatus } from '@/components/web/webRuntimeDisplay';
import { ENTER_CLASS } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { ApiError } from '@/lib/api';
import { formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';

import type { CapabilityState } from '@/components/web/capability';
import type { Node, NodeHealth } from '@/api/types';
import type { ReactNode } from 'react';

/** The public port the WEB transport is reached on: the HTTPS front, which is where it lives. */
const WEB_PUBLIC_PORT = 443;

function Row({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 bg-card px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-3">
      <span className="flex min-w-0 flex-col">
        <span className="truncate text-label text-mute">{label}</span>
        {hint && <span className="text-micro text-mute">{hint}</span>}
      </span>
      <span className="shrink-0 text-mono">{children}</span>
    </div>
  );
}

const CAPACITY_RESOURCES = ['http_connections', 'http_overload_connections', 'http_handlers', 'queue_items'] as const;

function CapacityPanel({ health, loading }: { health?: NodeHealth; loading: boolean }) {
  const { t, i18n } = useTranslation();
  const capacity = health?.web_runtime?.capacity ?? null;
  const events = capacity ? Object.values(capacity.overload_outcomes).reduce((sum, value) => sum + value, 0) : null;
  const resources = capacity?.resources.filter((resource) => CAPACITY_RESOURCES.includes(resource.resource as typeof CAPACITY_RESOURCES[number])) ?? [];

  return (
    <Panel>
      <PanelHeader icon={Gauge} title={t('web.capacity_title')} meta={capacity ? t(`web.overload_action_${capacity.connection_capacity_action}`, { defaultValue: capacity.connection_capacity_action }) : undefined} />
      <PanelBody className="space-y-4">
        {loading ? <Skeleton className="h-32 w-full" /> : !capacity ? (
          <p className="text-label text-mute">{t('web.capacity_unavailable')}</p>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <div><p className="text-micro text-mute">{t('web.capacity_saturated')}</p><p className={cn('mt-1 text-title', capacity.saturated_resources.length ? 'text-err' : 'text-ok')}>{capacity.saturated_resources.length ? formatNumber(capacity.saturated_resources.length, i18n.language) : t('web.capacity_none')}</p></div>
              <div><p className="text-micro text-mute">{t('web.capacity_events')}</p><p className="mt-1 text-title"><MetricValue value={events} /></p></div>
              <div><p className="text-micro text-mute">{t('web.capacity_pending_sessions')}</p><p className="mt-1 text-title"><MetricValue value={null} /></p></div>
              <div><p className="text-micro text-mute">{t('web.capacity_pending_streams')}</p><p className="mt-1 text-title"><MetricValue value={null} /></p></div>
            </div>
            <div className="space-y-3">
              {resources.map((resource) => {
                const percent = resource.limit > 0 ? Math.min(100, (resource.used / resource.limit) * 100) : 0;
                const saturated = capacity.saturated_resources.includes(resource.resource);
                return <div key={resource.resource}><div className="mb-1.5 flex items-center justify-between gap-3 text-label"><span>{t(`web.capacity_resource_${resource.resource}`, { defaultValue: resource.resource })}</span><span className={cn('mono text-mono', saturated ? 'text-err' : 'text-mute')}>{formatNumber(resource.used, i18n.language)} / {formatNumber(resource.limit, i18n.language)}</span></div><div className="h-1.5 overflow-hidden rounded-pill bg-hairline"><div className={cn('h-full rounded-pill', saturated ? 'bg-err' : percent >= 80 ? 'bg-warn' : 'bg-brand')} style={{width:`${percent}%`}} /></div></div>;
              })}
            </div>
            <p className="text-micro text-mute">{t('web.capacity_metric_note')}</p>
          </>
        )}
      </PanelBody>
    </Panel>
  );
}

/** A switch telemt either has on, has off, or that the panel could not settle for this node. */
function Toggle({ state, on }: { state: CapabilityState; on: boolean | null | undefined }) {
  const { t } = useTranslation();

  if (state === 'unsupported') return <span className="text-mono text-mute">{t('web.state_unsupported')}</span>;
  if (state === 'undetermined' || on === null || on === undefined) {
    return <MetricValue value={null} />;
  }
  return <span className={cn('text-mono', on ? 'text-ok' : 'text-mute')}>{t(on ? 'web.state_on' : 'web.state_off')}</span>;
}

/** What the panel's own prerequisite check last said about the front's certificate. */
function TlsValidity({ node }: { node: Node }) {
  const cert = node.last_check?.results?.find((r) => r.name === 'tls_cert');
  if (!cert) return <MetricValue value={null} />;
  return (
    <span className={cn('text-mono', cert.ok ? 'text-ok' : 'text-err')}>{cert.detail || (cert.ok ? 'ok' : 'failed')}</span>
  );
}

function StatusPanel({ node, health, offline, loading }: { node: Node; health?: NodeHealth; offline: boolean; loading: boolean }) {
  const { t } = useTranslation();
  const runtime = health?.web_runtime ?? null;
  const negotiation = nodeCapability(node, CAP_CARRIER_NEGOTIATION);
  const learning = nodeCapability(node, CAP_CARRIER_LEARNING);

  return (
    <Panel>
      <PanelHeader icon={Globe} title={t('web.status_title')} />
      {loading ? (
        <div className="space-y-3 p-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-4 w-full" />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-px bg-hairline sm:grid-cols-2">
          <Row label={t('web.status_transport')}>
            <StatusIndicator status={webRuntimeStatus(runtime, offline)} />
          </Row>
          <Row label={t('web.status_endpoint')} hint={t('web.status_endpoint_hint')}>
            <span className="mono text-mono text-foreground">
              {node.hostname}:{WEB_PUBLIC_PORT}
            </span>
          </Row>
          <Row label={t('web.status_tls')} hint={t('web.status_tls_hint')}>
            <TlsValidity node={node} />
          </Row>
          <Row label={t('web.status_negotiation')} hint={t('web.status_negotiation_hint')}>
            <Toggle state={negotiation} on={runtime?.carrier_negotiation} />
          </Row>
          <Row label={t('web.status_learning')} hint={t('web.status_learning_hint')}>
            <Toggle state={learning} on={runtime?.learning?.enabled} />
          </Row>
          <Row label={t('web.status_learning_entries')} hint={t('web.status_learning_entries_hint')}>
            {learning === 'unsupported' ? (
              <span className="text-mono text-mute">{t('web.state_unsupported')}</span>
            ) : (
              <span className="mono text-mono">
                <MetricValue value={runtime?.learning?.entries ?? null} />
                {runtime?.learning?.capacity ? <span className="text-mute"> / {runtime.learning.capacity}</span> : null}
              </span>
            )}
          </Row>
        </div>
      )}
      <WebLifecycleControls node={node} health={health} />
    </Panel>
  );
}

/** The WEB transport tab: what the node's WEB proxy is doing, what it learned, and a way to test it. */
export function NodeWebTab({ node }: { node: Node }) {
  const { t, i18n } = useTranslation();
  const healthQuery = useNodeHealth(node.id, node.online);
  const carriersQuery = useNodeWebCarriers(node.id);
  const web = nodeCapability(node, CAP_WEB);

  const offline = !node.online || (healthQuery.error instanceof ApiError && healthQuery.error.code === 'node_offline');
  const stats = carriersQuery.data;

  if (node.engine !== 'telemt') {
    return (
      <div className={cn(ENTER_CLASS, 'flex flex-col gap-4')}>
        <Panel>
          <PanelHeader icon={Globe} title={t('web.status_title')} />
          <CapabilityNotice state="unsupported" feature={t('web.feature_web')} />
        </Panel>
      </div>
    );
  }

  return (
    <div className={cn(ENTER_CLASS, 'flex flex-col gap-4')}>
      {web === 'unsupported' || web === 'undetermined' ? (
        <Panel>
          <PanelHeader icon={Globe} title={t('web.status_title')} />
          <CapabilityNotice state={web} feature={t('web.feature_web')} checkedAt={node.telemt_capabilities_checked_at} />
        </Panel>
      ) : (
        <StatusPanel node={node} health={healthQuery.data} offline={offline} loading={healthQuery.isLoading && !offline} />
      )}

      {web === 'supported' && <CapacityPanel health={healthQuery.data} loading={healthQuery.isLoading && !offline} />}

      <Panel>
        <PanelHeader
          icon={Radar}
          title={t('web.carriers_title')}
          meta={t('web.carriers_window', { hours: formatNumber(24, i18n.language) })}
        />
        <PanelBody className="space-y-5">
          {carriersQuery.isLoading ? (
            <>
              <Skeleton className="h-2.5 w-full rounded-pill" />
              <Skeleton className="h-24 w-full" />
            </>
          ) : carriersQuery.isError ? (
            <ErrorState
              inset
              message={carriersQuery.error instanceof ApiError ? carriersQuery.error.message : t('common.error_generic')}
              retryLabel={t('common.refresh')}
              onRetry={() => void carriersQuery.refetch()}
            />
          ) : !stats ? (
            <ErrorState
              inset
              message={t('common.error_generic')}
              retryLabel={t('common.refresh')}
              onRetry={() => void carriersQuery.refetch()}
            />
          ) : (
            <>
              <CarrierDistribution distribution={stats.carrier_selection_distribution} />
              <WebCounters stats={stats} />
            </>
          )}
        </PanelBody>
      </Panel>

      <WebPolicyCard nodeId={node.id} enabled={negotiationSupported(node)} />
      <WebDiagnosticsCard nodeId={node.id} />
    </div>
  );
}

function negotiationSupported(node: Node) { return nodeCapability(node, CAP_CARRIER_NEGOTIATION) === 'supported'; }
