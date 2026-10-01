import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';

import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { cn } from '@/lib/utils';

import { MAX_IMPORT, parseImport } from './importList';

import type { ImportLine } from './importList';
import type { UserFormValues } from './userForm';
import type { FieldErrors, UseFormRegister } from 'react-hook-form';

const PLACEHOLDER = [
  'ivan: 0123456789abcdef0123456789abcdef',
  'anna = "fedcba9876543210fedcba9876543210"',
  'tg://proxy?server=old.example.com&port=443&secret=ee…',
].join('\n');

interface ImportFieldsProps {
  register: UseFormRegister<UserFormValues>;
  errors: FieldErrors<UserFormValues>;
  text: string;
  lineErrors: Record<number, string>;
}

function shortSecret(secret: string): string {
  return secret ? `${secret.slice(0, 6)}…${secret.slice(-4)}` : '';
}

export function ImportFields({ register, errors, text, lineErrors }: ImportFieldsProps) {
  const { t } = useTranslation();
  const parsed = useMemo(() => parseImport(text), [text]);
  const broken = parsed.lines.filter((l) => l.problem || lineErrors[l.line]).length;

  const problemOf = (l: ImportLine): string => {
    if (lineErrors[l.line]) return lineErrors[l.line];
    if (l.problem === 'duplicate') return t('users.import_duplicate', { line: l.same_as });
    if (l.problem) return t(`users.import_${l.problem}`);
    return '';
  };

  const listError = errors.import_text?.message;

  return (
    <>
      <div className="space-y-2">
        <Label htmlFor="user-import">{t('users.import_list')}</Label>
        <Textarea
          id="user-import"
          autoFocus
          rows={8}
          spellCheck={false}
          autoComplete="off"
          className="mono text-mono"
          placeholder={PLACEHOLDER}
          {...register('import_text')}
          aria-invalid={!!listError}
        />
        {listError === 'empty' ? (
          <p className="text-label text-destructive">{t('users.import_empty')}</p>
        ) : listError === 'lines' ? (
          <p className="text-label text-destructive">{t('users.import_fix_lines')}</p>
        ) : listError === 'too_many' ? (
          <p className="text-label text-destructive">{t('users.import_too_many', { max: MAX_IMPORT })}</p>
        ) : (
          <p className="text-label text-mute">{t('users.import_list_hint')}</p>
        )}
      </div>

      {parsed.lines.length > 0 && (
        <div className="space-y-2">
          <p className="text-label text-mute">
            {t('users.import_found', { count: parsed.lines.length - broken })}
            {broken > 0 && <span className="text-destructive">{`, ${t('users.import_broken', { count: broken })}`}</span>}
          </p>
          <ul className="max-h-64 divide-y divide-hairline overflow-y-auto rounded-control border border-hairline">
            {parsed.lines.map((l) => {
              const problem = problemOf(l);
              return (
                <li key={l.line} className="grid grid-cols-[2rem_minmax(0,1fr)_auto] items-baseline gap-x-3 px-3 py-1.5">
                  <span className="mono text-mono text-mute">{l.line}</span>
                  <span className="min-w-0">
                    <span className={cn('block truncate text-body', l.auto_name && 'text-mute')}>{l.label || '—'}</span>
                    {l.owner_label && <span className="block truncate text-label text-mute">{l.owner_label}</span>}
                    {problem && <span className="block text-label text-destructive">{problem}</span>}
                  </span>
                  <span className="mono text-mono text-mute">{shortSecret(l.secret)}</span>
                </li>
              );
            })}
          </ul>
        </div>
      )}

      {parsed.ready.length > 0 && (
        <div className="space-y-2 rounded-control border border-hairline-strong p-3">
          <p className="text-body font-semibold">{t('users.import_move_title')}</p>
          <ol className="list-decimal space-y-1.5 pl-5 text-label text-mute">
            <li>
              {parsed.servers.length > 0
                ? t('users.import_move_address', { servers: parsed.servers.join(', ') })
                : t('users.import_move_address_any')}
            </li>
            <li>
              {parsed.domains.length > 0
                ? t('users.import_move_domains', { domains: parsed.domains.join(', ') })
                : t('users.import_move_domains_any')}
            </li>
            <li>{t('users.import_move_links')}</li>
          </ol>
        </div>
      )}

      <div className="space-y-2">
        <Label htmlFor="user-note">
          {t('keys.field_note')} <span className="font-normal text-mute">({t('keys.field_optional')})</span>
        </Label>
        <Textarea id="user-note" rows={2} {...register('note')} />
      </div>
    </>
  );
}
