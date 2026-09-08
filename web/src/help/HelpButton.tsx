import { CircleHelp } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

import { HelpSheet } from './HelpSheet';

import type { HelpTopic } from './content';

interface HelpButtonProps {
  topic: HelpTopic;
  className?: string;
}

/**
 * The `?` icon next to a title. Ghost, dim, and labelled for screen readers;
 * it never competes with the actions beside it.
 *
 * The sheet it opens is rendered right here rather than at the shell, on
 * purpose: inside a modal dialog the help must be a *nested* dialog in React's
 * tree, or the dialog underneath reads a click in the help as an outside press
 * and closes itself - form and all. `data-help-button` is what the `?`
 * shortcut looks for to open the help of whichever dialog is on top.
 */
export function HelpButton({ topic, className }: HelpButtonProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className={cn('text-mute', className)}
        aria-label={t('help.open')}
        title={t('help.open')}
        data-help-button={topic}
        onClick={() => setOpen(true)}
      >
        <CircleHelp />
      </Button>
      <HelpSheet topic={topic} open={open} onOpenChange={setOpen} />
    </>
  );
}
