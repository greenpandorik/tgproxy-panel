import { ChevronRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Link, useLocation } from 'react-router-dom';

import { navGroupOf, navItemForPath } from '@/components/shell/nav';

import type { ReactNode } from 'react';

interface PageHeaderProps {
  title: string;
  /** Description below the title: scope, guidance or freshness. */
  description?: ReactNode;
  actions?: ReactNode;
}

function Breadcrumbs({ title }: { title: string }) {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  const section = navItemForPath(pathname);
  const group = section && navGroupOf(section);
  const crumbs: { label: string; to?: string }[] = [{ label: t('shell.breadcrumb_root'), to: '/' }];
  if (section && section.to !== '/') {
    if (group?.qualified) crumbs.push({ label: t(group.labelKey) });
    crumbs.push({ label: t(section.labelKey), to: section.to });
    if (pathname !== section.to) crumbs.push({ label: title });
  } else {
    crumbs.push({ label: title });
  }

  return (
    <nav aria-label={t('shell.breadcrumbs')} className="flex min-w-0 flex-wrap items-center gap-1 text-label text-mute">
      {crumbs.map((c, i) => (
        <span key={`${c.label}-${i}`} className="flex min-w-0 items-center gap-1">
          {i > 0 && <ChevronRight className="size-3.5 shrink-0 opacity-60" aria-hidden="true" />}
          {c.to && i < crumbs.length - 1 ? (
            <Link to={c.to} className="truncate hover:text-foreground" title={c.label}>
              {c.label}
            </Link>
          ) : (
            <span className="truncate" title={c.label} aria-current={i === crumbs.length - 1 ? 'page' : undefined}>
              {c.label}
            </span>
          )}
        </span>
      ))}
    </nav>
  );
}

export function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <div className="flex flex-col items-stretch justify-between gap-3 sm:flex-row sm:items-start">
      <div className="flex min-w-0 flex-col gap-1.5 sm:flex-1">
        <h1 className="text-display wrap-break-word text-foreground">{title}</h1>
        <Breadcrumbs title={title} />
        {description && <p className="mt-1 max-w-[72ch] text-body text-mute">{description}</p>}
      </div>
      {actions && (
        <div className="flex flex-wrap items-center gap-2 sm:shrink-0 sm:pt-1 [&>[data-help-button]]:order-last">{actions}</div>
      )}
    </div>
  );
}
