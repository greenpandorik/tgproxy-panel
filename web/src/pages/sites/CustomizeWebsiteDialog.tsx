import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { siteKeys } from '@/api/sites';
import type { SiteTemplate } from '@/api/types';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';

export function CustomizeWebsiteDialog({ website, onClose }: { website: SiteTemplate; onClose: () => void }) {
  const { t } = useTranslation(); const qc = useQueryClient(); const navigate = useNavigate();
  const [name,setName] = useState(`${website.display_name ?? website.name} · ${t('sites.custom_copy')}`);
  const [values,setValues] = useState(website.variables ?? {});
  const save = useMutation({mutationFn: () => api.post<SiteTemplate>(`/api/v1/site-templates/${website.id}/customize`,{name,variables:values}),onSuccess:(site)=>{void qc.invalidateQueries({queryKey:siteKeys.all});onClose();navigate(`/sites/${site.id}`);}});
  return <Dialog open onOpenChange={(open)=>{if(!open && !save.isPending)onClose();}}><DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-xl"><DialogHeader><DialogTitle>{t('sites.customize')}</DialogTitle><DialogDescription>{t('sites.customize_hint')}</DialogDescription></DialogHeader>
    <form className="space-y-4" onSubmit={(e)=>{e.preventDefault();save.mutate();}}>
      <div className="space-y-2"><Label htmlFor="custom-name">{t('sites.import_name')}</Label><Input id="custom-name" required value={name} onChange={(e)=>setName(e.target.value)} /></div>
      {Object.entries(values).map(([key,value])=><div key={key} className="space-y-2"><Label htmlFor={`custom-${key}`}>{t(`sites.variable_${key}`)}</Label><Input id={`custom-${key}`} type={key==='contact_email'?'email':'text'} maxLength={1000} value={value} onChange={(e)=>setValues({...values,[key]:e.target.value})} /></div>)}
      {save.isError && <p role="alert" className="text-destructive">{save.error.message}</p>}
      <Button disabled={save.isPending || !name.trim()} type="submit">{t('sites.create_copy')}</Button>
    </form>
  </DialogContent></Dialog>;
}
