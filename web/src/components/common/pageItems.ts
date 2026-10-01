export type PageItem = number | 'gap';

/** The page buttons to draw: both ends, the current page with its neighbours, and a gap for each skipped run. */
export function pageItems(page: number, totalPages: number): PageItem[] {
  const keep = new Set([1, totalPages, page - 1, page, page + 1]);
  if (page <= 3) for (const p of [2, 3]) keep.add(p);
  if (page >= totalPages - 2) for (const p of [totalPages - 2, totalPages - 1]) keep.add(p);
  const pages = [...keep].filter((p) => p >= 1 && p <= totalPages).sort((a, b) => a - b);
  const out: PageItem[] = [];
  let prev = 0;
  for (const p of pages) {
    if (p - prev === 2) out.push(prev + 1);
    else if (p - prev > 2) out.push('gap');
    out.push(p);
    prev = p;
  }
  return out;
}
