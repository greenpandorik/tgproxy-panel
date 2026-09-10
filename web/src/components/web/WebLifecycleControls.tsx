import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '@/lib/api';
import { useAuth } from '@/auth/AuthProvider';
import { nodeKeys } from '@/api/nodes';
import type { Node, NodeHealth } from '@/api/types';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { nodeCapability } from './capability';

export function WebLifecycleControls({node,health}:{node:Node;health?:NodeHealth}) {
 const {t}=useTranslation();const {isWriter}=useAuth();const qc=useQueryClient();
 const [action,setAction]=useState<string|null>(null);
 const mutation=useMutation({mutationFn:(value:string)=>api.post(`/api/v1/nodes/${node.id}/web/lifecycle`,{action:value,timeout_secs:120}),onSuccess:()=>qc.invalidateQueries({queryKey:nodeKeys.health(node.id)})});
 const lifecycle=health?.web_runtime?.lifecycle;
 if(!isWriter)return null;
 return <div className="space-y-3 border-t border-hairline p-4">
  <p className="text-label text-mute">{t('web.lifecycle_hint')}</p>
  <div className="flex flex-wrap gap-2">{([['pause','WebPause'],['drain','WebDrain'],['resume','WebResume'],['reset_learning','CarrierLearningReset']] as const).map(([key,cap])=><Button key={key} size="sm" variant="outline" disabled={!node.online || mutation.isPending || nodeCapability(node,cap)!=='supported'} onClick={()=>setAction(key)}>{t(`web.lifecycle_${key}`)}</Button>)}</div>
  {lifecycle?.drain && <p role="status" className="text-label">{t(`web.lifecycle_state_${lifecycle.state}`,lifecycle.state)} · {t('web.update_remaining',{sessions:lifecycle.drain.remaining_sessions,streams:lifecycle.drain.remaining_streams})}</p>}
  {mutation.isError && <p role="alert" className="text-destructive">{mutation.error.message}</p>}
  {mutation.isSuccess && <p role="status" className="text-label text-mute">{t('web.lifecycle_accepted')}</p>}
  <ConfirmDialog open={!!action} onOpenChange={(open)=>{if(!open)setAction(null);}} title={t(`web.lifecycle_${action}`)} description={t(`web.lifecycle_confirm_${action}`)} confirmLabel={t(`web.lifecycle_${action}`)} onConfirm={()=>mutation.mutateAsync(action!).then(()=>undefined)} />
 </div>;
}
