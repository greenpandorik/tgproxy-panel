import { CircleHelp, KeyRound, Languages, Moon, Plus, Send, Server, Sun } from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useLocation, useNavigate } from 'react-router-dom';

import { useKeys } from '@/api/keys';
import { useApplyDirtyNodes, useNodes } from '@/api/nodes';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { StatusBadge } from '@/components/common/StatusBadge';
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from '@/components/ui/command';
import { toast } from '@/components/ui/toast';
import { topicForPath, useHelp } from '@/help';
import { setLang } from '@/i18n';
import { useTheme } from '@/theme/ThemeProvider';
import { nodeStatus } from '@/pages/nodes/nodeDisplay';

import { NAV_ITEMS } from './nav';

const RESULT_LIMIT = 6;

interface CommandPaletteProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function CommandPalette({ open, onOpenChange }: CommandPaletteProps) {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const help = useHelp();
  const pageTopic = topicForPath(pathname);
  const { theme, toggleTheme } = useTheme();
  const [query, setQuery] = useState('');
  const [debounced, setDebounced] = useState('');
  const [applyConfirmOpen, setApplyConfirmOpen] = useState(false);

  const nodesQuery = useNodes();
  const nodes = useMemo(() => nodesQuery.data?.items ?? [], [nodesQuery.data]);
  const keysQuery = useKeys(debounced ? { q: debounced, per_page: RESULT_LIMIT } : {});
  const applyDirty = useApplyDirtyNodes();

  useEffect(() => {
    const id = window.setTimeout(() => setDebounced(query.trim()), 200);
    return () => window.clearTimeout(id);
  }, [query]);

  // Closing always clears what was typed, so the palette never reopens mid-search.
  const setOpen = useCallback(
    (next: boolean) => {
      if (!next) setQuery('');
      onOpenChange(next);
    },
    [onOpenChange],
  );

  // ⌘K on macOS, Ctrl+K elsewhere. Toggles, so the same chord closes it.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen(!open);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, setOpen]);

  const term = debounced.toLowerCase();
  const matchedNodes = useMemo(
    () =>
      (term ? nodes.filter((n) => n.name.toLowerCase().includes(term) || n.hostname.toLowerCase().includes(term)) : nodes).slice(
        0,
        RESULT_LIMIT,
      ),
    [nodes, term],
  );
  const matchedKeys = (keysQuery.data?.items ?? []).slice(0, RESULT_LIMIT);
  const dirtyNodeIds = useMemo(() => nodes.filter((n) => n.dirty).map((n) => n.id), [nodes]);

  const run = (fn: () => void) => {
    setOpen(false);
    fn();
  };

  // "Apply everywhere" restarts relays and drops live sessions, so it asks first.
  const requestApplyEverywhere = () => {
    if (dirtyNodeIds.length === 0) {
      toast.add({ description: t('command.apply_all_nothing'), type: 'success' });
      return;
    }
    setApplyConfirmOpen(true);
  };

  const applyEverywhere = () =>
    new Promise<void>((resolve) => {
      applyDirty.mutate(dirtyNodeIds, {
        onSuccess: ({ queued, total }) =>
          toast.add({
            description: t('command.apply_all_queued', { queued, total }),
            type: queued === total ? 'success' : 'error',
          }),
        onError: () => toast.add({ description: t('common.error_generic'), type: 'error' }),
        onSettled: () => resolve(),
      });
    });

  const nextLang = i18n.language?.startsWith('en') ? 'ru' : 'en';

  return (
    <>
      <ConfirmDialog
        open={applyConfirmOpen}
        onOpenChange={setApplyConfirmOpen}
        title={t('command.apply_all_confirm_title', { count: dirtyNodeIds.length })}
        description={t('command.apply_all_confirm_description')}
        confirmLabel={t('command.action_apply_all')}
        onConfirm={applyEverywhere}
      />
      <CommandDialog open={open} onOpenChange={setOpen} title={t('command.title')} description={t('command.description')}>
        <Command shouldFilter={false} loop>
          <CommandInput value={query} onValueChange={setQuery} placeholder={t('command.placeholder')} />
          <CommandList>
            <CommandEmpty>{t('command.empty')}</CommandEmpty>

            <CommandGroup heading={t('command.group_sections')}>
              {NAV_ITEMS.filter((item) => !term || t(item.labelKey).toLowerCase().includes(term)).map((item) => (
                <CommandItem key={item.to} value={`section:${item.to}`} onSelect={() => run(() => navigate(item.to))}>
                  <item.icon strokeWidth={1.8} aria-hidden="true" />
                  <span className="truncate">{t(item.labelKey)}</span>
                </CommandItem>
              ))}
            </CommandGroup>

            {matchedNodes.length > 0 && (
              <CommandGroup heading={t('command.group_nodes')}>
                {matchedNodes.map((node) => (
                  <CommandItem key={node.id} value={`node:${node.id}`} onSelect={() => run(() => navigate(`/nodes/${node.id}`))}>
                    <Server strokeWidth={1.8} aria-hidden="true" />
                    {/* Persisted status, the same source the rail and the list read. */}
                    <StatusBadge status={nodeStatus(node)} hideLabel />
                    <span className="truncate">{node.name}</span>
                    <CommandShortcut>{node.hostname}</CommandShortcut>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            {matchedKeys.length > 0 && (
              <CommandGroup heading={t('command.group_keys')}>
                {matchedKeys.map((k) => (
                  <CommandItem key={k.id} value={`key:${k.id}`} onSelect={() => run(() => navigate(`/keys?key=${k.id}`))}>
                    <KeyRound strokeWidth={1.8} aria-hidden="true" />
                    <span className="truncate">{k.label}</span>
                    {k.owner_label && <CommandShortcut>{k.owner_label}</CommandShortcut>}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            <CommandGroup heading={t('command.group_actions')}>
              <CommandItem value="action:create-key" onSelect={() => run(() => navigate('/keys?create=1'))}>
                <Plus strokeWidth={1.8} aria-hidden="true" />
                {t('command.action_create_key')}
              </CommandItem>
              <CommandItem value="action:apply-all" onSelect={() => run(requestApplyEverywhere)}>
                <Send strokeWidth={1.8} aria-hidden="true" />
                {t('command.action_apply_all')}
                {dirtyNodeIds.length > 0 && <CommandShortcut>{dirtyNodeIds.length}</CommandShortcut>}
              </CommandItem>
              {help && pageTopic && (
                <CommandItem value="action:help" onSelect={() => run(() => help.open(pageTopic))}>
                  <CircleHelp strokeWidth={1.8} aria-hidden="true" />
                  {t('help.page_help')}
                  <CommandShortcut>?</CommandShortcut>
                </CommandItem>
              )}
              <CommandItem value="action:theme" onSelect={() => run(toggleTheme)}>
                {theme === 'dark' ? <Sun strokeWidth={1.8} aria-hidden="true" /> : <Moon strokeWidth={1.8} aria-hidden="true" />}
                {t('command.action_toggle_theme')}
              </CommandItem>
              <CommandItem value="action:lang" onSelect={() => run(() => setLang(nextLang))}>
                <Languages strokeWidth={1.8} aria-hidden="true" />
                {t('command.action_toggle_lang')}
                <CommandShortcut>{nextLang}</CommandShortcut>
              </CommandItem>
            </CommandGroup>
          </CommandList>
        </Command>
      </CommandDialog>
    </>
  );
}
