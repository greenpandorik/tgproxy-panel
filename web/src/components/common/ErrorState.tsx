import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

/**
 * What a surface shows when the query behind it failed.
 *
 * An empty list and a failed one look identical unless one of them says so,
 * and the only useful move at that point is to ask again - so this is a
 * sentence, a red dot to say which of the three states it is, and the retry.
 *
 * There is one of these, deliberately. Before the phase 8 fix wave the same
 * fourteen classes, the same role="alert", the same 7px dot and the same
 * outline retry button were re-typed in five files and the in-panel version in
 * four more, at three different paddings, which meant the error surface had
 * nine owners and no definition.
 *
 * `inset` is the in-panel form: no border and no radius, because the panel
 * already draws both, and the tighter 4-based padding the rest of a panel body
 * uses. Everything else is the page-level form that stands where a list would
 * have been.
 */
export function ErrorState({
  message,
  retryLabel,
  onRetry,
  inset = false,
  className,
}: {
  message: string;
  retryLabel: string;
  onRetry: () => void;
  inset?: boolean;
  className?: string;
}) {
  return (
    <div
      role="alert"
      className={cn(
        'flex flex-col items-center justify-center gap-3 text-center',
        inset ? 'px-6 py-10' : 'rounded-surface border border-hairline py-14',
        className,
      )}
    >
      <p className={cn('flex items-center justify-center gap-2 text-body text-mute', !inset && 'px-6')}>
        <span className="size-[7px] shrink-0 rounded-pill bg-err" aria-hidden="true" />
        {message}
      </p>
      <Button type="button" variant="outline" size="sm" onClick={onRetry}>
        {retryLabel}
      </Button>
    </div>
  );
}
