import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Eye, LayoutTemplate } from 'lucide-react';
import type { SiteTemplate } from '@/api/types';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import type { ReactNode } from 'react';

const CATEGORIES = ['all', 'saas', 'studio', 'editorial', 'local', 'portfolio', 'corporate', 'status_docs', 'custom'];
export function WebsiteGallery({ websites, onPreview, onUse, actions, currentId }: { websites: SiteTemplate[]; onPreview: (site: SiteTemplate) => void; onUse?: (site: SiteTemplate) => void; actions?: (site: SiteTemplate) => ReactNode; currentId?: string | null }) {
  const { t } = useTranslation();
  const [category, setCategory] = useState('all');
  const [search, setSearch] = useState('');
  const visible = websites.filter((site) => {
    const group = site.category ?? (site.is_preset ? 'corporate' : 'custom');
    return (category === 'all' || group === category || (category === 'status_docs' && ['status', 'docs'].includes(group))) && `${site.name} ${site.display_name ?? ''}`.toLocaleLowerCase().includes(search.toLocaleLowerCase());
  });
  return <div className="space-y-4">
    <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
      <div className="flex flex-wrap gap-1" aria-label={t('sites.categories')}>{CATEGORIES.map((key) => <Button key={key} size="sm" variant={category === key ? 'secondary' : 'ghost'} aria-pressed={category === key} onClick={() => setCategory(key)}>{t(`sites.category_${key}`)}</Button>)}</div>
      <Input className="xl:max-w-64" value={search} onChange={(e) => setSearch(e.target.value)} aria-label={t('sites.search')} placeholder={t('sites.search')} type="search" />
    </div>
    {visible.length === 0 ? <p className="p-8 text-center text-mute">{t('sites.no_matches')}</p> : <ul className="grid grid-cols-1 gap-5 sm:grid-cols-2 xl:grid-cols-3">
      {visible.map((site) => <li key={site.id} className="min-w-0 overflow-hidden rounded-surface border border-hairline bg-card">
        <button type="button" className="group relative block aspect-[16/10] w-full overflow-hidden border-b border-hairline bg-background focus-visible:outline-2 focus-visible:outline-ring" onClick={() => onPreview(site)} aria-label={t('sites.preview_title', { name: site.display_name ?? site.name })}>
          {site.screenshot_url ? <img src={site.screenshot_url} alt="" loading="lazy" width={960} height={600} className="h-full w-full object-cover object-top transition-transform duration-200 group-hover:scale-[1.02] motion-reduce:transform-none" /> : <span className="flex h-full flex-col items-center justify-center gap-3 text-mute"><LayoutTemplate size={36} /><span>{t('sites.custom_preview')}</span></span>}
        </button>
        <div className="space-y-3 p-4">
          <div className="flex items-start justify-between gap-2"><div className="min-w-0"><h2 className="truncate font-semibold">{site.display_name ?? site.name}</h2><p className="mt-1 text-label text-mute">{t(`sites.category_${site.category ?? (site.is_preset ? 'corporate' : 'custom')}`)}</p></div>{actions?.(site)}{site.id === currentId && <Badge>{t('sites.current')}</Badge>}</div>
          <p className="text-label text-mute">{t('sites.used_by', { count: site.used_by ?? 0 })}</p>
          <div className="flex gap-2"><Button variant="outline" size="sm" onClick={() => onPreview(site)}><Eye />{t('sites.preview')}</Button>{onUse && <Button size="sm" variant="secondary" onClick={() => onUse(site)}>{t('sites.use')}</Button>}</div>
        </div>
      </li>)}
    </ul>}
  </div>;
}
