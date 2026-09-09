import { ExternalLink } from 'lucide-react';
import { useRef } from 'react';
import { useTranslation } from 'react-i18next';

import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet';

import { docsUrl, HELP_TOPICS } from './content';

import type { HelpTopic } from './content';

interface HelpSheetProps {
  topic: HelpTopic;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** The notes of a topic, in order. i18next hands the object back; a missing block is an empty list. */
function useNotes(topic: HelpTopic): string[] {
  const { t } = useTranslation();
  const raw = t(`help.topics.${topic}.notes`, { returnObjects: true, defaultValue: '' });
  if (!raw || typeof raw !== 'object') return [];
  return Object.values(raw as Record<string, string>).filter((v) => typeof v === 'string' && v !== '');
}

export function HelpSheet({ topic, open, onOpenChange }: HelpSheetProps) {
  const { t, i18n } = useTranslation();
  const def = HELP_TOPICS[topic];
  const base = `help.topics.${topic}`;
  const notes = useNotes(topic);
  const more = docsUrl(topic, i18n.language ?? 'ru');
  const bodyRef = useRef<HTMLDivElement>(null);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      {/* Focus the scroll body on open: base-ui would otherwise focus the first
          tabbable element, the "read more" link at the very bottom, and scroll
          the whole body to it. */}
      <SheetContent
        side="right"
        className="w-full gap-0 data-[side=right]:sm:max-w-[420px]"
        aria-label={t('help.open')}
        initialFocus={bodyRef}
      >
        <SheetHeader className="pr-12">
          <p className="micro text-mute">{t('help.title')}</p>
          <SheetTitle>{t(`${base}.title`)}</SheetTitle>
          <SheetDescription className="pt-1 text-body">{t(`${base}.intro`)}</SheetDescription>
        </SheetHeader>

        <div ref={bodyRef} tabIndex={-1} className="min-h-0 flex-1 overflow-y-auto outline-none">
          {def.fields.length > 0 && (
            <section className="border-b border-hairline px-4 py-3">
              <h3 className="micro mb-2 text-mute">{t('help.section_fields')}</h3>
              <dl className="divide-y divide-hairline">
                {def.fields.map((field) => {
                  const fb = `${base}.fields.${field}`;
                  const example = i18n.exists(`${fb}.example`) ? t(`${fb}.example`) : '';
                  const tip = i18n.exists(`${fb}.tip`) ? t(`${fb}.tip`) : '';
                  return (
                    <div key={field} className="py-3 first:pt-0 last:pb-0" data-help-field={field}>
                      <dt className="text-body font-medium text-foreground">{t(`${fb}.name`)}</dt>
                      <dd className="mt-1 space-y-1 text-body text-mute">
                        <p>{t(`${fb}.what`)}</p>
                        {example && (
                          <p className="flex items-baseline gap-2">
                            <span className="micro shrink-0 text-mute">{t('help.example')}</span>
                            <code className="mono text-mono break-all text-foreground">{example}</code>
                          </p>
                        )}
                        {tip && (
                          <p className="flex items-start gap-2 text-label">
                            <span className="mt-1.5 size-[7px] shrink-0 rounded-pill bg-info" aria-hidden="true" />
                            <span>{tip}</span>
                          </p>
                        )}
                      </dd>
                    </div>
                  );
                })}
              </dl>
            </section>
          )}

          {notes.length > 0 && (
            <section className="border-b border-hairline px-4 py-3">
              <h3 className="micro mb-2 text-mute">{t('help.section_notes')}</h3>
              <ul className="space-y-1.5">
                {notes.map((note, i) => (
                  <li key={i} className="flex items-start gap-2 text-body text-mute">
                    <span className="mt-2 size-[7px] shrink-0 rounded-pill bg-warn" aria-hidden="true" />
                    <span>{note}</span>
                  </li>
                ))}
              </ul>
            </section>
          )}

          {more && (
            <p className="px-4 py-3">
              <a
                href={more}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1.5 text-body text-brand-ink underline-offset-4 hover:underline"
              >
                {t('help.more')}
                <ExternalLink className="size-3.5" aria-hidden="true" />
              </a>
            </p>
          )}
        </div>

        <p className="mono border-t border-hairline px-4 py-3 text-mono text-mute">{t('help.shortcut_hint')}</p>
      </SheetContent>
    </Sheet>
  );
}
