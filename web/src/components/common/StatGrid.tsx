import { enter } from '@/components/ui/motion';
import { cn } from '@/lib/utils';

import { StatTile } from './StatTile';

import type { StatTileProps } from './StatTile';

/** A tile plus the key it is rendered under, which is also its i18n-free id. */
export type StatGridTile = StatTileProps & { id: string };

/** Responsive overview metrics, two columns on mobile and four on desktop. */
export function StatGrid({ tiles, className }: { tiles: StatGridTile[]; className?: string }) {
  return (
    <div className={cn('grid grid-cols-2 gap-3 sm:grid-cols-2 lg:grid-cols-4', className)}>
      {tiles.map((tile, i) => {
        const { id, ...props } = tile;
        return <StatTile key={id} {...props} {...enter(i)} />;
      })}
    </div>
  );
}
