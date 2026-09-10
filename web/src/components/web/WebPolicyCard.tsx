import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { SlidersHorizontal } from 'lucide-react';
import { api } from '@/lib/api';
import { useAuth } from '@/auth/AuthProvider';
import { useApplyNode, nodeKeys } from '@/api/nodes';
import { Panel, PanelHeader, PanelBody } from '@/components/common/Panel';
import { ErrorState } from '@/components/common/ErrorState';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';

interface WebPolicy {
  preset: string; carrier: string; carriers: string[] | false; carrier_learning: boolean;
  carrier_negotiation_aggressiveness: string;
  overload: { preset: 'balanced' | 'high_load' | 'custom'; connection_capacity_action: 'drop' | 'wait' | 'respond' };
  timeouts: { carrier_negotiation_deadlines_secs: number[]; carrier_health_secs: number; carrier_learning_secs: number; bridge_request_secs: number; bridge_retry_secs: number; carrier_probe_coalesce_ms: number };
}
interface PolicyResponse { policy: WebPolicy; default: WebPolicy; overridden: boolean }
const PRESETS = ['automatic', 'compatibility', 'prefer_websocket', 'https_only', 'custom'];

export function WebPolicyCard({ nodeId, enabled }: { nodeId: string; enabled: boolean }) {
  const query = useQuery({queryKey:['web-policy',nodeId],queryFn:()=>api.get<PolicyResponse>(`/api/v1/nodes/${nodeId}/web-policy`)});
  const { t } = useTranslation();
  return <Panel><PanelHeader icon={SlidersHorizontal} title={t('web.policy_title')} /><PanelBody>
    {query.isLoading ? <Skeleton className="h-44" /> : query.isError ? <ErrorState message={query.error.message} onRetry={()=>void query.refetch()} retryLabel={t('common.refresh')} /> : query.data && <PolicyForm key={JSON.stringify(query.data.policy)} nodeId={nodeId} data={query.data} enabled={enabled} />}
  </PanelBody></Panel>;
}
function PolicyForm({nodeId,data,enabled}:{nodeId:string;data:PolicyResponse;enabled:boolean}) {
  const {t}=useTranslation();const {isWriter}=useAuth();const qc=useQueryClient();
  const [policy,setPolicy]=useState(data.policy);const apply=useApplyNode(nodeId);
  const save=useMutation({mutationFn:()=>api.put<PolicyResponse>(`/api/v1/nodes/${nodeId}/web-policy`,policy),onSuccess:()=>{void qc.invalidateQueries({queryKey:['web-policy',nodeId]});void qc.invalidateQueries({queryKey:nodeKeys.one(nodeId)});apply.mutate();}});
  const dirty=JSON.stringify(policy)!==JSON.stringify(data.policy);
  const selectPreset=(preset:string)=>{
    if(preset==='custom'){setPolicy({...policy,preset});return;}
    const next=structuredClone(data.default);next.preset=preset;
    if(preset==='compatibility'){next.carriers=['https-lanes','websocket','websocket-lanes'];next.carrier_learning=false;}
    if(preset==='https_only'){next.carriers=false;next.carrier_learning=false;}
    if(preset==='prefer_websocket'){next.carriers=['websocket','websocket-lanes','https-lanes'];}
    setPolicy(next);
  };
  const numericFields=[['carrier_health_secs',1,86400],['carrier_learning_secs',2,86400],['bridge_request_secs',1,60],['bridge_retry_secs',1,300],['carrier_probe_coalesce_ms',0,10]] as const;
  const selectOverloadPreset=(preset:WebPolicy['overload']['preset'])=>{
    const action = preset === 'balanced' ? 'wait' : preset === 'high_load' ? 'respond' : policy.overload.connection_capacity_action;
    setPolicy({...policy,overload:{preset,connection_capacity_action:action}});
  };
  return <form className="space-y-4" onSubmit={(e)=>{e.preventDefault();save.mutate();}}>
    <p className="max-w-3xl text-label text-mute">{t('web.policy_hint')}</p>
    <fieldset disabled={!isWriter || !enabled || save.isPending} className="space-y-4 disabled:opacity-60">
      <div className="flex flex-wrap gap-2">{PRESETS.map((preset)=><Button type="button" key={preset} variant={policy.preset===preset?'secondary':'outline'} aria-pressed={policy.preset===preset} onClick={()=>selectPreset(preset)}>{t(`web.preset_${preset}`)}</Button>)}</div>
      <p className="text-label">{t('web.policy_order')}: <span className="font-medium">{policy.carriers===false?'HTTPS': [...new Set([...policy.carriers,policy.carrier])].join(' → ')}</span></p>
      <details><summary className="cursor-pointer text-label text-mute">{t('web.policy_advanced')}</summary><div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
        <section className="space-y-3 rounded-control border border-hairline bg-background p-3 sm:col-span-2 xl:col-span-3">
          <div><p className="text-label font-medium text-foreground">{t('web.overload_title')}</p><p className="mt-1 text-micro text-mute">{t('web.overload_hint')}</p></div>
          <div className="flex flex-wrap gap-2">{(['balanced','high_load','custom'] as const).map((preset)=><Button type="button" size="sm" key={preset} variant={policy.overload.preset===preset?'secondary':'outline'} aria-pressed={policy.overload.preset===preset} onClick={()=>selectOverloadPreset(preset)}>{t(`web.overload_preset_${preset}`)}</Button>)}</div>
          <div className="max-w-sm space-y-2"><Label htmlFor="web-capacity-action">{t('web.overload_action')}</Label><select id="web-capacity-action" className="h-9 w-full rounded-control border border-hairline bg-card px-2" value={policy.overload.connection_capacity_action} onChange={(e)=>setPolicy({...policy,overload:{preset:'custom',connection_capacity_action:e.target.value as WebPolicy['overload']['connection_capacity_action']}})}>{(['wait','respond','drop'] as const).map((v)=><option key={v} value={v}>{t(`web.overload_action_${v}`)}</option>)}</select><p className="text-micro text-mute">{t(`web.overload_action_${policy.overload.connection_capacity_action}_hint`)}</p></div>
        </section>
        <label className="flex items-center gap-2"><input type="checkbox" checked={policy.carrier_learning} onChange={(e)=>setPolicy({...policy,preset:'custom',carrier_learning:e.target.checked})} />{t('web.status_learning')}</label>
        <div className="space-y-2"><Label htmlFor="web-aggressiveness">{t('web.learning_policy')}</Label><select id="web-aggressiveness" className="h-9 w-full rounded-control border border-hairline bg-background px-2" value={policy.carrier_negotiation_aggressiveness} onChange={(e)=>setPolicy({...policy,preset:'custom',carrier_negotiation_aggressiveness:e.target.value})}>{['conservative','balanced','aggressive'].map((v)=><option key={v} value={v}>{t(`web.aggression_${v}`)}</option>)}</select></div>
        {numericFields.map(([key,min,max])=><div key={key} className="space-y-2"><Label htmlFor={`web-${key}`}>{t(`web.timeout_${key}`)}</Label><Input id={`web-${key}`} type="number" required min={key==='bridge_retry_secs'?policy.timeouts.bridge_request_secs:min} max={max} step={1} value={policy.timeouts[key]} onChange={(e)=>setPolicy({...policy,preset:'custom',timeouts:{...policy.timeouts,[key]:e.target.valueAsNumber}})} /><p className="text-micro text-mute">{min}–{max}</p></div>)}
        <div className="space-y-2 sm:col-span-2"><Label>{t('web.deadlines')}</Label><div className="grid grid-cols-4 gap-2">{policy.timeouts.carrier_negotiation_deadlines_secs.map((value,i)=><Input key={i} type="number" required step={1} min={i===0?1:policy.timeouts.carrier_negotiation_deadlines_secs[i-1]+1} aria-label={`${t('web.deadlines')} ${i+1}`} value={value} onChange={(e)=>setPolicy({...policy,preset:'custom',timeouts:{...policy.timeouts,carrier_negotiation_deadlines_secs:policy.timeouts.carrier_negotiation_deadlines_secs.map((n,index)=>index===i?e.target.valueAsNumber:n)}})} />)}</div></div>
      </div></details>
    </fieldset>
    {save.isError && <p className="text-destructive" role="alert">{save.error.message}</p>}
    {apply.isError && <p className="text-destructive" role="alert">{apply.error.message}</p>}
    {isWriter && <div className="flex flex-wrap items-center gap-3"><Button type="submit" disabled={!dirty || !enabled || save.isPending}>{t('web.policy_save')}</Button>{dirty && <Button variant="ghost" onClick={()=>setPolicy(data.policy)}>{t('common.cancel')}</Button>}<span className="text-label text-mute">{t('web.policy_apply_hint')}</span></div>}
  </form>;
}
