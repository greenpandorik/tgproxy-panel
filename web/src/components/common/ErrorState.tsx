import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

// What a surface shows when the query behind it failed.
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
        inset ? 'px-6 py-10' : 'rounded-surface border border-hairline-strong py-14',
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
