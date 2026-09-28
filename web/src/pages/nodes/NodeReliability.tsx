import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useReliability, useSaveReliability } from '@/api/reliability';
import type { RecoveryPolicy, RecoveryState, ReliabilityReport } from '@/api/reliability';
import type { Node } from '@/api/types';
import { useAuth } from '@/auth/AuthProvider';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { ErrorState } from '@/components/common/ErrorState';
import { AdvancedSettings } from '@/components/common/AdvancedSettings';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { ApiError } from '@/lib/api';
import { formatDateTime } from '@/lib/format';

function PolicyForm({ node, state }: { node: Node; state: RecoveryState }) {
  const { t } = useTranslation();
  const { isWriter } = useAuth();
  const [policy, setPolicy] = useState<RecoveryPolicy>(state.policy);
  const [saved, setSaved] = useState(false);
  const save = useSaveReliability(node.id);
  const change = <K extends keyof RecoveryPolicy>(key: K, value: RecoveryPolicy[K]) => {
    setSaved(false);
    setPolicy((p) => ({ ...p, [key]: value }));
  };
  const dirty = JSON.stringify(policy) !== JSON.stringify(state.policy);
  const restorePrimary =
    !dirty &&
    policy.egress !== 'unmanaged' &&
    state.active !== (policy.egress === 'socks5' ? policy.socks_address : policy.egress);
  return (
    <form
      className="space-y-5"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate({ ...policy, restore_primary: restorePrimary }, { onSuccess: () => setSaved(true) });
      }}
    >
      <fieldset disabled={!isWriter || save.isPending} className="space-y-5 disabled:opacity-70">
        <div className="space-y-2">
          <Label htmlFor="recovery-mode">{t('reliability.recovery')}</Label>
          <select
            id="recovery-mode"
            value={policy.recovery}
            onChange={(e) => change('recovery', e.target.value as RecoveryPolicy['recovery'])}
            className="ops-select"
          >
            <option value="observe">{t('reliability.observe')}</option>
            <option value="restart">{t('reliability.restart')}</option>
          </select>
          <p className="text-label text-mute">{t('reliability.recovery_hint')}</p>
        </div>
        <label className="flex items-start gap-3 text-body">
          <input
            type="checkbox"
            className="mt-1 size-4 accent-primary"
            checked={policy.maintenance}
            onChange={(e) => change('maintenance', e.target.checked)}
          />
          <span>
            {t('reliability.maintenance')}
            <span className="mt-1 block text-label text-mute">{t('reliability.maintenance_hint')}</span>
          </span>
        </label>
        <AdvancedSettings label={t('reliability.thresholds')}>
          <div className="grid gap-4 sm:grid-cols-3">
            {(['failure_threshold', 'cooldown_seconds', 'max_actions_hour'] as const).map((key) => (
              <div className="space-y-2" key={key}>
                <Label htmlFor={key}>{t(`reliability.${key}`)}</Label>
                <Input
                  id={key}
                  type="number"
                  min={key === 'failure_threshold' ? 2 : key === 'cooldown_seconds' ? 60 : 1}
                  max={key === 'failure_threshold' ? 20 : key === 'cooldown_seconds' ? 86400 : 6}
                  value={policy[key]}
                  onChange={(e) => change(key, Number(e.target.value))}
                  required
                />
              </div>
            ))}
          </div>
        </AdvancedSettings>
        <AdvancedSettings label={t('reliability.routing_toggle')} defaultOpen={state.policy.egress !== 'unmanaged'} className="border-t border-hairline pt-5">
          <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="egress-mode">{t('reliability.egress')}</Label>
            <select
              id="egress-mode"
              className="ops-select"
              value={policy.egress}
              onChange={(e) => {
                change('egress', e.target.value as RecoveryPolicy['egress']);
                if (e.target.value === 'unmanaged') change('automatic_failover', false);
              }}
            >
              <option value="unmanaged" disabled={state.policy.egress !== 'unmanaged' && state.active !== 'direct'}>
                {t('reliability.unmanaged')}
              </option>
              <option value="direct">{t('reliability.direct')}</option>
              <option value="socks5">{t('reliability.socks5')}</option>
            </select>
            <p className="max-w-[72ch] text-label text-mute">{t('reliability.egress_hint')}</p>
          </div>
          {policy.egress === 'socks5' && (
            <div className="space-y-2">
              <Label htmlFor="primary-socks">{t('reliability.primary')}</Label>
              <Input
                id="primary-socks"
                placeholder="127.0.0.1:1080"
                value={policy.socks_address}
                onChange={(e) => change('socks_address', e.target.value)}
                required
              />
            </div>
          )}
          {policy.egress !== 'unmanaged' && (
            <>
              <label className="flex items-center gap-3 text-body">
                <input
                  type="checkbox"
                  className="size-4 accent-primary"
                  checked={policy.automatic_failover}
                  onChange={(e) => change('automatic_failover', e.target.checked)}
                />
                {t('reliability.failover')}
              </label>
              {policy.automatic_failover && (
                <div className="space-y-2">
                  <Label htmlFor="reserve-socks">{t('reliability.reserve')}</Label>
                  <Input
                    id="reserve-socks"
                    placeholder="127.0.0.1:1081"
                    value={policy.reserve_socks_address}
                    onChange={(e) => change('reserve_socks_address', e.target.value)}
                    required
                  />
                  <p className="text-label text-mute">{t('reliability.reserve_hint')}</p>
                </div>
              )}
            </>
          )}
          </div>
        </AdvancedSettings>
      </fieldset>
      <p className="text-label text-mute">{t('reliability.apply_hint')}</p>
      {save.isError && (
        <p role="alert" className="text-body text-err">
          {save.error instanceof ApiError ? save.error.message : t('common.error_generic')}
        </p>
      )}
      <div className="flex items-center gap-3">
        {isWriter && (
          <Button
            type="submit"
            disabled={
              (!dirty &&
                (policy.egress === 'unmanaged' ||
                  state.active === (policy.egress === 'socks5' ? policy.socks_address : policy.egress))) ||
              save.isPending
            }
          >
            {t(save.isPending ? 'common.loading' : restorePrimary ? 'reliability.restore_primary' : 'common.save')}
          </Button>
        )}
        {saved && (
          <p role="status" className="text-label text-ok">
            {t('reliability.saved')}
          </p>
        )}
      </div>
    </form>
  );
}
export function NodeReliability({ node }: { node: Node }) {
  const { t } = useTranslation();
  const query = useReliability(node.id, node.online);
  return (
    <Panel>
      <PanelHeader title={t('reliability.title')} />
      <PanelBody>
        {!node.online ? (
          <p className="text-body text-mute">{t('nodes.offline_message')}</p>
        ) : query.isLoading ? (
          <Skeleton className="h-48 w-full" />
        ) : query.isError ? (
          <ErrorState
            inset
            message={query.error instanceof ApiError ? query.error.message : t('common.error_generic')}
            retryLabel={t('common.refresh')}
            onRetry={() => void query.refetch()}
          />
        ) : query.data ? (
          <PolicyForm node={node} state={query.data} />
        ) : null}
      </PanelBody>
    </Panel>
  );
}
export function ReliabilityReadings({ report }: { report?: ReliabilityReport | null }) {
  const { t, i18n } = useTranslation();
  if (!report) return null;
  const resources = report.resources;
  const value = (v: number | null | undefined) => (v == null ? t('common.not_available') : String(v));
  const readings = [
    ['fd', resources.fd_used == null ? t('common.not_available') : `${resources.fd_used} / ${value(resources.fd_limit)}`],
    [
      'conntrack',
      resources.conntrack_used == null
        ? t('common.not_available')
        : `${resources.conntrack_used} / ${value(resources.conntrack_limit)}`,
    ],
    [
      'inodes',
      resources.inodes_used_percent == null ? t('common.not_available') : `${resources.inodes_used_percent.toFixed(1)}%`,
    ],
    ['retransmits', value(resources.tcp_retransmits_total)],
    ['drops', value(resources.listen_drops_total)],
    ['oom', value(resources.oom_kills_total)],
  ];
  return (
    <Panel>
      <PanelHeader title={t('reliability.readings')} />
      <PanelBody className="space-y-4">
        <p className="text-label text-mute">
          {formatDateTime(report.at, i18n.language)} · {t('reliability.counters_hint')}
        </p>
        <dl className="grid gap-x-6 sm:grid-cols-2 lg:grid-cols-3">
          {readings.map(([key, v]) => (
            <div key={key} className="flex flex-wrap justify-between gap-2 border-b border-hairline py-3">
              <dt className="text-label text-mute">{t(`reliability.${key}`)}</dt>
              <dd className="mono text-mono">{v}</dd>
            </div>
          ))}
        </dl>
        {report.routes.map((r, index) => (
          <div key={`${r.kind}-${index}`} className="flex flex-wrap justify-between gap-2 text-body">
            <span>{r.kind}</span>
            <span>
              {r.age_seconds > 120 ? t('reliability.stale') : t(r.healthy ? 'reliability.healthy' : 'reliability.failed')}
            </span>
          </div>
        ))}
        {report.events.length > 0 && (
          <AdvancedSettings label={t('reliability.events')}>
            <ul className="divide-y divide-hairline">
              {report.events
                .slice(-10)
                .reverse()
                .map((ev, i) => (
                  <li key={i} className="py-3 text-label">
                    <time className="text-mute">{formatDateTime(ev.at, i18n.language)}</time>
                    <p>
                      {ev.action}: {ev.result}
                    </p>
                  </li>
                ))}
            </ul>
          </AdvancedSettings>
        )}
      </PanelBody>
    </Panel>
  );
}
