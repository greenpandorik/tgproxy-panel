import { enter } from '@/components/ui/motion';
import { cn } from '@/lib/utils';

import { StatTile } from './StatTile';

import type { StatTileProps } from './StatTile';

/** A tile plus the key it is rendered under, which is also its i18n-free id. */
export type StatGridTile = StatTileProps & { id: string };

/**
 * The dashboard's row of facts.
 *
 * Four columns, not five. Eleven tiles land 4 + 4 + 3 either way - measured,
 * the grid is the same height and the chart below it starts at the same y -
 * but five columns leave the last row four fifths empty, which reads as a
 * layout that ran out of content rather than as a set of eleven facts.
 * Below `lg` it drops to three and below `sm` to two, because a display-role
 * number and a label do not survive a third of a phone.
 *
 * The tiles carry the panel's one entrance, staggered and capped at six by
 * `enter`, so the row says "this just loaded" without turning into a show.
 */
export function StatGrid({ tiles, className }: { tiles: StatGridTile[]; className?: string }) {
  return (
    <div className={cn('grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4', className)}>
      {tiles.map((tile, i) => {
        const { id, ...props } = tile;
        return <StatTile key={id} {...props} {...enter(i)} />;
      })}
    </div>
  );
}
