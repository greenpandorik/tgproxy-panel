import { Trans, useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';
import { formatDraftTime } from '@/lib/drafts';
import { cn } from '@/lib/utils';

interface DraftBannerProps {
  /** When the draft was last written (epoch ms). */
  savedAt: number;
  /** "Continue": the caller loads the draft into the form. */
  onResume: () => void;
  /** "Start over": the caller drops the draft; the form is already pristine. */
  onDiscard: () => void;
  className?: string;
}

/**
 * "Draft from 12:30 · Continue / Start over", at the top of a form that was
 * closed with something typed in it. A fact about the form, not a warning, so
 * it gets the same hairline row and 7px dot the rest of the panel uses for
 * state; the timestamp is mono like every other time on screen.
 */
export function DraftBanner({ savedAt, onResume, onDiscard, className }: DraftBannerProps) {
  const { t, i18n } = useTranslation();

  return (
    <div
      role="status"
      data-slot="draft-banner"
      className={cn(
        'flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border border-hairline bg-background px-3 py-2',
        className,
      )}
    >
      <p className="flex min-w-0 items-center gap-2 text-xs text-mute">
        <span className="size-[7px] shrink-0 rounded-full bg-pending" aria-hidden="true" />
        <span>
          <Trans
            i18nKey="drafts.banner"
            values={{ time: formatDraftTime(savedAt, i18n.language) }}
            components={{ time: <span className="mono text-foreground" /> }}
          />
        </span>
      </p>
      <div className="ml-auto flex items-center gap-1">
        <Button type="button" variant="outline" size="xs" onClick={onResume}>
          {t('drafts.resume')}
        </Button>
        <Button type="button" variant="ghost" size="xs" onClick={onDiscard}>
          {t('drafts.discard')}
        </Button>
      </div>
    </div>
  );
}
