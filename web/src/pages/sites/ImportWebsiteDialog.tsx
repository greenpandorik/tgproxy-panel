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

export function ImportWebsiteDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState('');
  const [file, setFile] = useState<File | null>(null);
  const upload = useMutation({ mutationFn: () => {
    const body = new FormData(); body.set('name', name.trim()); if (file) body.set('file', file);
    return api.post<SiteTemplate>('/api/v1/site-templates/import', body);
  }, onSuccess: (site) => { void qc.invalidateQueries({ queryKey: siteKeys.all }); onOpenChange(false); setName(''); setFile(null); navigate(`/sites/${site.id}`); } });
  return <Dialog open={open} onOpenChange={(next) => { if (!upload.isPending) { upload.reset(); onOpenChange(next); } }}><DialogContent className="sm:max-w-lg"><DialogHeader><DialogTitle>{t('sites.import_zip')}</DialogTitle><DialogDescription>{t('sites.import_hint')}</DialogDescription></DialogHeader>
    <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); upload.mutate(); }}>
      <div className="space-y-2"><Label htmlFor="import-name">{t('sites.import_name')}</Label><Input id="import-name" required maxLength={120} value={name} onChange={(e) => setName(e.target.value)} /></div>
      <div className="space-y-2"><Label htmlFor="import-file">ZIP</Label><Input id="import-file" type="file" required accept=".zip,application/zip" onChange={(e) => setFile(e.target.files?.[0] ?? null)} /></div>
      {upload.isError && <p role="alert" className="text-destructive">{upload.error.message}</p>}
      <Button type="submit" disabled={!file || !name.trim() || upload.isPending}>{t(upload.isPending ? 'sites.importing' : 'sites.import_submit')}</Button>
    </form>
  </DialogContent></Dialog>;
}
