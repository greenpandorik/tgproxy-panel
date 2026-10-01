import { ChevronLeft, ChevronRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

import { pageItems } from './pageItems';

interface PaginationProps {
  /** 1-based. */
  page: number;
  pageSize: number;
  total: number;
  onPageChange: (page: number) => void;
  /** With both of these the footer offers a choice of how many rows a page holds. */
  pageSizes?: readonly number[];
  onPageSizeChange?: (size: number) => void;
  className?: string;
}

/** The footer of a paged list: which rows are shown, how many per page, and the pages themselves. */
export function Pagination({ page, pageSize, total, onPageChange, pageSizes, onPageSizeChange, className }: PaginationProps) {
  const { t } = useTranslation();
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const from = total === 0 ? 0 : Math.min(total, (page - 1) * pageSize + 1);
  const to = Math.min(total, page * pageSize);
  const step = 'h-8 min-w-8 px-2 text-label';

  return (
    <div className={cn('flex flex-wrap items-center gap-x-4 gap-y-3', className)}>
      <p className="mono mr-auto text-mono text-mute">{t('common.pagination.shown', { from, to, total })}</p>
      {pageSizes && onPageSizeChange && (
        <label className="flex items-center gap-2 text-label text-mute">
          {t('common.pagination.page_size')}
          <select
            className="ops-select mono min-h-8 w-auto py-0 text-mono"
            value={pageSize}
            onChange={(e) => onPageSizeChange(Number(e.target.value))}
          >
            {pageSizes.map((size) => (
              <option key={size} value={size}>
                {size}
              </option>
            ))}
          </select>
        </label>
      )}
      {totalPages > 1 && (
        <nav aria-label={t('common.pagination.label')} className="flex items-center gap-1">
          <Button
            type="button"
            variant="outline"
            size="sm"
            className={step}
            disabled={page <= 1}
            onClick={() => onPageChange(page - 1)}
            aria-label={t('common.pagination.prev')}
          >
            <ChevronLeft />
          </Button>
          {pageItems(page, totalPages).map((item, i) =>
            item === 'gap' ? (
              <span key={`gap-${i}`} aria-hidden="true" className="mono w-6 text-center text-mono text-mute">
                …
              </span>
            ) : (
              <Button
                key={item}
                type="button"
                variant={item === page ? 'default' : 'outline'}
                size="sm"
                className={cn(step, 'mono text-mono')}
                aria-current={item === page ? 'page' : undefined}
                aria-label={t('common.pagination.page', { page: item })}
                onClick={() => item !== page && onPageChange(item)}
              >
                {item}
              </Button>
            ),
          )}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className={step}
            disabled={page >= totalPages}
            onClick={() => onPageChange(page + 1)}
            aria-label={t('common.pagination.next')}
          >
            <ChevronRight />
          </Button>
        </nav>
      )}
    </div>
  );
}
