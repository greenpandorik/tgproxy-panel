import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '@/lib/api';
import { useAuth } from '@/auth/AuthProvider';
import { useApplyNode, nodeKeys } from '@/api/nodes';
import { AdvancedSettings } from '@/components/common/AdvancedSettings';
import { ErrorState } from '@/components/common/ErrorState';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';

import { useWebPolicy, webPolicyKey } from './webPolicy';

import type { CapabilityState } from './capability';
import type { PolicyResponse, WebPolicy } from './webPolicy';

const PRESETS = ['automatic', 'compatibility', 'prefer_websocket', 'https_only', 'custom'];

/** How a telemt server picks the WEB transport for a new connection, and how it behaves when full. */
export function WebPolicyCard({ nodeId, capability }: { nodeId: string; capability: CapabilityState }) {
  const query = useWebPolicy(nodeId);
  const { t } = useTranslation();
  if (query.isLoading) return <Skeleton className="h-44" />;
  if (query.isError) {
    return (
      <ErrorState inset message={query.error.message} onRetry={() => void query.refetch()} retryLabel={t('common.refresh')} />
    );
  }
  return query.data ? (
    <PolicyForm key={JSON.stringify(query.data.policy)} nodeId={nodeId} data={query.data} capability={capability} />
  ) : null;
}
function PolicyForm({ nodeId, data, capability }: { nodeId: string; data: PolicyResponse; capability: CapabilityState }) {
  const enabled = capability === 'supported';
  const { t } = useTranslation();
  const { isWriter } = useAuth();
  const qc = useQueryClient();
  const [policy, setPolicy] = useState(data.policy);
  const apply = useApplyNode(nodeId);
  const save = useMutation({
    mutationFn: () => api.put<PolicyResponse>(`/api/v1/nodes/${nodeId}/web-policy`, policy),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: webPolicyKey(nodeId) });
      void qc.invalidateQueries({ queryKey: nodeKeys.one(nodeId) });
      apply.mutate();
    },
  });
  const dirty = JSON.stringify(policy) !== JSON.stringify(data.policy);
  const selectPreset = (preset: string) => {
    if (preset === 'custom') {
      setPolicy({ ...policy, preset });
      return;
    }
    const next = structuredClone(data.default);
    next.preset = preset;
    if (preset === 'compatibility') {
      next.carriers = ['https-lanes', 'websocket', 'websocket-lanes'];
      next.carrier_learning = false;
    }
    if (preset === 'https_only') {
      next.carriers = false;
      next.carrier_learning = false;
    }
    if (preset === 'prefer_websocket') {
      next.carriers = ['websocket', 'websocket-lanes', 'https-lanes'];
    }
    setPolicy(next);
  };
  const numericFields = [
    ['carrier_health_secs', 1, 86400],
    ['carrier_learning_secs', 2, 86400],
    ['bridge_request_secs', 1, 60],
    ['bridge_retry_secs', 1, 300],
    ['carrier_probe_coalesce_ms', 0, 10],
  ] as const;
  const selectOverloadPreset = (preset: WebPolicy['overload']['preset']) => {
    const action =
      preset === 'balanced' ? 'wait' : preset === 'high_load' ? 'respond' : policy.overload.connection_capacity_action;
    setPolicy({ ...policy, overload: { preset, connection_capacity_action: action } });
  };
  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <p className="max-w-[72ch] text-label text-mute">{t('web.policy_hint')}</p>
      {!enabled && (
        <p role="status" className="flex max-w-[72ch] items-start gap-2 text-label text-warn">
          <span className="mt-1.5 size-[7px] shrink-0 rounded-pill bg-warn" aria-hidden="true" />
          {t(capability === 'unsupported' ? 'web.policy_unsupported' : 'web.policy_undetermined')}
        </p>
      )}
      <fieldset disabled={!isWriter || !enabled || save.isPending} className="space-y-4">
        <div className="flex flex-wrap gap-2">
          {PRESETS.map((preset) => (
            <Button
              type="button"
              key={preset}
              variant={policy.preset === preset ? 'default' : 'outline'}
              aria-pressed={policy.preset === preset}
              onClick={() => selectPreset(preset)}
            >
              {t(`web.preset_${preset}`)}
            </Button>
          ))}
        </div>
        <p className="text-label text-mute">
          {t('web.policy_order')}:{' '}
          <span className="mono text-mono text-foreground">
            {policy.carriers === false ? 'https' : [...new Set([...policy.carriers, policy.carrier])].join(' → ')}
          </span>
        </p>
        <AdvancedSettings label={t('web.policy_advanced')}>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
            <section className="space-y-3 rounded-control border border-hairline bg-background p-3 sm:col-span-2 xl:col-span-3">
              <div>
                <p className="text-label font-medium text-foreground">{t('web.overload_title')}</p>
                <p className="mt-1 text-micro text-mute">{t('web.overload_hint')}</p>
              </div>
              <div className="flex flex-wrap gap-2">
                {(['balanced', 'high_load', 'custom'] as const).map((preset) => (
                  <Button
                    type="button"
                    size="sm"
                    key={preset}
                    variant={policy.overload.preset === preset ? 'default' : 'outline'}
                    aria-pressed={policy.overload.preset === preset}
                    onClick={() => selectOverloadPreset(preset)}
                  >
                    {t(`web.overload_preset_${preset}`)}
                  </Button>
                ))}
              </div>
              <div className="max-w-sm space-y-2">
                <Label htmlFor="web-capacity-action">{t('web.overload_action')}</Label>
                <select
                  id="web-capacity-action"
                  className="ops-select"
                  value={policy.overload.connection_capacity_action}
                  onChange={(e) =>
                    setPolicy({
                      ...policy,
                      overload: {
                        preset: 'custom',
                        connection_capacity_action: e.target.value as WebPolicy['overload']['connection_capacity_action'],
                      },
                    })
                  }
                >
                  {(['wait', 'respond', 'drop'] as const).map((v) => (
                    <option key={v} value={v}>
                      {t(`web.overload_action_${v}`)}
                    </option>
                  ))}
                </select>
                <p className="text-micro text-mute">
                  {t(`web.overload_action_${policy.overload.connection_capacity_action}_hint`)}
                </p>
              </div>
            </section>
            <label className="flex min-h-10 items-center gap-3 self-end text-body">
              <input
                type="checkbox"
                className="size-4 accent-primary"
                checked={policy.carrier_learning}
                onChange={(e) => setPolicy({ ...policy, preset: 'custom', carrier_learning: e.target.checked })}
              />
              {t('web.status_learning')}
            </label>
            <div className="space-y-2">
              <Label htmlFor="web-aggressiveness">{t('web.learning_policy')}</Label>
              <select
                id="web-aggressiveness"
                className="ops-select"
                value={policy.carrier_negotiation_aggressiveness}
                onChange={(e) => setPolicy({ ...policy, preset: 'custom', carrier_negotiation_aggressiveness: e.target.value })}
              >
                {['conservative', 'balanced', 'aggressive'].map((v) => (
                  <option key={v} value={v}>
                    {t(`web.aggression_${v}`)}
                  </option>
                ))}
              </select>
            </div>
            {numericFields.map(([key, min, max]) => (
              <div key={key} className="space-y-2">
                <Label htmlFor={`web-${key}`}>{t(`web.timeout_${key}`)}</Label>
                <Input
                  id={`web-${key}`}
                  type="number"
                  required
                  min={key === 'bridge_retry_secs' ? policy.timeouts.bridge_request_secs : min}
                  max={max}
                  step={1}
                  value={policy.timeouts[key]}
                  onChange={(e) =>
                    setPolicy({ ...policy, preset: 'custom', timeouts: { ...policy.timeouts, [key]: e.target.valueAsNumber } })
                  }
                />
                <p className="text-micro text-mute">
                  {min}–{max}
                </p>
              </div>
            ))}
            <div className="space-y-2 sm:col-span-2">
              <Label>{t('web.deadlines')}</Label>
              <div className="grid grid-cols-4 gap-2">
                {policy.timeouts.carrier_negotiation_deadlines_secs.map((value, i) => (
                  <Input
                    key={i}
                    type="number"
                    required
                    step={1}
                    min={i === 0 ? 1 : policy.timeouts.carrier_negotiation_deadlines_secs[i - 1] + 1}
                    aria-label={`${t('web.deadlines')} ${i + 1}`}
                    value={value}
                    onChange={(e) =>
                      setPolicy({
                        ...policy,
                        preset: 'custom',
                        timeouts: {
                          ...policy.timeouts,
                          carrier_negotiation_deadlines_secs: policy.timeouts.carrier_negotiation_deadlines_secs.map(
                            (n, index) => (index === i ? e.target.valueAsNumber : n),
                          ),
                        },
                      })
                    }
                  />
                ))}
              </div>
            </div>
          </div>
        </AdvancedSettings>
      </fieldset>
      {save.isError && (
        <p className="text-body text-destructive" role="alert">
          {save.error.message}
        </p>
      )}
      {apply.isError && (
        <p className="text-body text-destructive" role="alert">
          {apply.error.message}
        </p>
      )}
      {isWriter && (
        <div className="flex flex-wrap items-center gap-3">
          <Button type="submit" disabled={!dirty || !enabled || save.isPending}>
            {t('web.policy_save')}
          </Button>
          {dirty && (
            <Button type="button" variant="ghost" onClick={() => setPolicy(data.policy)}>
              {t('common.cancel')}
            </Button>
          )}
          <span className="text-label text-mute">{t('web.policy_apply_hint')}</span>
        </div>
      )}
    </form>
  );
}
