import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSiteTemplate } from '@/api/sites';
import type { SiteTemplate } from '@/api/types';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { ErrorState } from '@/components/common/ErrorState';
import { Skeleton } from '@/components/ui/skeleton';
import { buildPreviewSrcdoc } from './templatePreview';

export function WebsitePreview({ website, onClose, onUse }: { website: SiteTemplate | null; onClose: () => void; onUse?: (site: SiteTemplate) => void }) {
  const { t } = useTranslation();
  const query = useSiteTemplate(website?.id ?? '');
  const [device, setDevice] = useState<'desktop' | 'tablet' | 'mobile'>('desktop');
  const width = { desktop: 1280, tablet: 768, mobile: 390 }[device];
  return <Dialog open={!!website} onOpenChange={(open) => { if (!open) onClose(); }}>
    <DialogContent className="max-h-[94dvh] sm:max-w-[min(94vw,1400px)]">
      <DialogHeader className="pr-8"><DialogTitle>{website?.display_name ?? website?.name}</DialogTitle><DialogDescription>{t('sites.preview_hint')}</DialogDescription></DialogHeader>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex gap-1" aria-label={t('sites.preview_size')}>{(['desktop', 'tablet', 'mobile'] as const).map((key) => <Button key={key} size="sm" variant={device === key ? 'secondary' : 'ghost'} aria-pressed={device === key} onClick={() => setDevice(key)}>{t(`sites.device_${key}`)}</Button>)}</div>
        {onUse && website && <Button onClick={() => onUse(website)}>{t('sites.use')}</Button>}
      </div>
      <div className="overflow-auto rounded-surface border border-hairline bg-background p-2">
        {query.isLoading ? <Skeleton className="h-[60dvh] w-full" /> : query.isError ? <ErrorState message={query.error.message} onRetry={() => void query.refetch()} retryLabel={t('common.refresh')} /> : <iframe title={t('sites.preview_title', { name: website?.display_name ?? website?.name })} sandbox="" srcDoc={buildPreviewSrcdoc(query.data?.html ?? '', query.data?.assets ?? {})} style={{ width }} className="mx-auto h-[65dvh] max-w-none border-0 bg-white" />}
      </div>
    </DialogContent>
  </Dialog>;
}
