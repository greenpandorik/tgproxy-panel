import { zodResolver } from '@hookform/resolvers/zod';
import { Ban, CircleAlert, Download, MoreHorizontal, Plus, Power, RefreshCw, RotateCw, Trash2, Undo2 } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';

import {
  subscriptionQrUrl,
  useBindKey,
  useCreateSubscription,
  useDeleteKey,
  useKey,
  useKeyLinks,
  usePatchKey,
  useRevokeKey,
  useRevokeSubscription,
  useRotateKey,
  useSetKeyDisabled,
  useUnbindKey,
} from '@/api/keys';
import { useNodes } from '@/api/nodes';
import { subscriptionBase, useSubscriptionService } from '@/api/subscriptionService';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { DraftBanner } from '@/components/common/DraftBanner';
import { SegmentedControl } from '@/components/common/SegmentedControl';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { serializeDraft, useDraft } from '@/lib/drafts';
import { formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import { KeyLinkDialog } from './KeyLinkDialog';
import { KeyStatsSection } from './KeyStatsSection';
import { StateBadge } from './StateBadge';
import { transportScope } from './transport';
import { AboutFields, ExpiryField, LimitsCardBody, ServersField } from './UserCards';
import { AccessSwitch, DirectLinksRow, SubscriptionBlock, UserFacts, UserPresence } from './UserOverview';
import { NEW_USER, patchPayload, userSchema, valuesFromUser } from './userForm';
import { USER_TABS, tabForErrors, tabsWithErrors } from './userTabs';

import type { UserTab } from './userTabs';
import type { UserFormValues } from './userForm';
import type { KeyStatsRange } from '@/api/keys';
import type { AccessKey } from '@/api/types';
import type { ReactNode } from 'react';

const FORM_ID = 'user-form';
const WINDOW_CLASS =
  'flex flex-col gap-0 overflow-hidden p-0 sm:h-[min(52rem,calc(100dvh-3rem))] sm:max-w-3xl max-sm:top-0 max-sm:left-0 max-sm:h-dvh max-sm:max-h-dvh max-sm:w-full max-sm:max-w-full max-sm:translate-x-0 max-sm:translate-y-0 max-sm:rounded-none max-sm:border-0';
const STATS_RANGES: KeyStatsRange[] = ['24h', '7d'];
const SERVER_FIELDS = ['label', 'owner_label', 'note', 'carrier_mode', 'expires_at', 'sub_slug', 'node_ids', 'limits', 'telemt_limits'] as const;
type ServerField = (typeof SERVER_FIELDS)[number];

const TAB_LABEL: Record<UserTab, string> = {
  main: 'users.tab_main',
  access: 'users.tab_access',
  limits: 'users.card_limits',
  stats: 'users.tab_stats',
};

function errText(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback;
}

const NOTICE_TONE = {
  welcome: { dot: 'bg-ok', text: 'rounded-control bg-elevated px-3 py-2 text-foreground' },
  warn: { dot: 'bg-warn', text: 'text-warn' },
  mute: { dot: 'bg-dim', text: 'text-mute' },
  err: { dot: 'bg-destructive', text: 'text-destructive' },
} as const;

function Notice({ tone, role, children }: { tone: keyof typeof NOTICE_TONE; role?: 'alert'; children: ReactNode }) {
  return (
    <p role={role} className={cn('flex items-start gap-2 text-label', NOTICE_TONE[tone].text)}>
      <span className={cn('mt-1.5 size-[7px] shrink-0 rounded-pill', NOTICE_TONE[tone].dot)} aria-hidden="true" />
      {children}
    </p>
  );
}

interface UserDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  keyId: string | null;
  /** Opened right after creation: the link to send comes first. */
  welcome?: boolean;
}

export function UserDialog({ open, onOpenChange, keyId, welcome }: UserDialogProps) {
  const { t, i18n } = useTranslation();
  const { isWriter } = useAuth();
  const id = keyId ?? '';
  const userQuery = useKey(id);
  const user = userQuery.data;
  const nodesQuery = useNodes();
  const patchKey = usePatchKey(id);
  const bindKey = useBindKey(id);
  const unbindKey = useUnbindKey(id);
  const setDisabled = useSetKeyDisabled();
  const rotateKey = useRotateKey();
  const revokeKey = useRevokeKey();
  const deleteKey = useDeleteKey();
  const issueLink = useCreateSubscription(id);
  const serviceQuery = useSubscriptionService();
  const revokeLink = useRevokeSubscription(id);

  const [tab, setTab] = useState<UserTab>('main');
  const [linksOpen, setLinksOpen] = useState(false);
  const [qrOpen, setQrOpen] = useState(false);
  const [statsRange, setStatsRange] = useState<KeyStatsRange>('24h');
  const [confirm, setConfirm] = useState<null | 'rotate' | 'reissue' | 'unlink' | 'revoke' | 'delete'>(null);
  const [focusRequest, setFocusRequest] = useState(0);
  const bodyRef = useRef<HTMLDivElement>(null);

  const {
    control,
    register,
    handleSubmit,
    reset,
    watch,
    setValue,
    setError,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<UserFormValues>({
    resolver: zodResolver(userSchema),
    defaultValues: { ...NEW_USER, creating: false },
    shouldFocusError: false,
  });

  const session = open && keyId ? keyId : '';
  const [tabSession, setTabSession] = useState(session);
  if (tabSession !== session) {
    setTabSession(session);
    setTab('main');
  }

  const baseline = user ? serializeDraft(valuesFromUser(user)) : '';
  const loaded = useRef({ session: '', baseline: '' });
  const dirtyRef = useRef(isDirty);
  useEffect(() => {
    dirtyRef.current = isDirty;
  }, [isDirty]);
  useEffect(() => {
    if (!session) loaded.current = { session: '', baseline: '' };
    if (!session || !baseline) return;
    const fresh = loaded.current.session !== session;
    if (!fresh && (loaded.current.baseline === baseline || dirtyRef.current)) return;
    loaded.current = { session, baseline };
    reset(JSON.parse(baseline) as UserFormValues);
  }, [session, baseline, reset]);

  useEffect(() => {
    if (!focusRequest) return;
    const target = bodyRef.current?.querySelector<HTMLElement>('[aria-invalid="true"], [data-field-error]');
    target?.focus();
    target?.scrollIntoView?.({ block: 'center' });
  }, [focusRequest]);

  const values = watch();
  const revoked = user?.state === 'revoked';
  const locked = revoked || !isWriter;
  const nodes = nodesQuery.data?.items ?? [];
  const scope = transportScope(nodes, values.node_ids);
  const boundIds = new Set((user?.nodes ?? []).map((n) => n.node_id));
  const measured = nodes.some((n) => n.engine === 'telemt' && boundIds.has(n.id));
  const linksQuery = useKeyLinks(id, open && isWriter && !!user && !revoked);
  const linksCount = linksQuery.data?.items.reduce((sum, group) => sum + group.links.length, 0);
  const errorTabs = tabsWithErrors(Object.keys(errors));

  const draft = useDraft<UserFormValues>(`user-edit-${id || 'none'}`, values, {
    initial: user ? valuesFromUser(user) : NEW_USER,
    open: open && !!user && !locked,
  });

  const resumeDraft = () => {
    if (!draft.draft) return;
    reset(draft.draft.value, { keepDefaultValues: true });
    draft.dismiss();
  };

  const routeErrors = (fields: Iterable<string>) => {
    const tabs = tabsWithErrors(fields);
    if (tabs.size === 0) return;
    setTab((current) => tabForErrors(current, tabs));
    setFocusRequest((n) => n + 1);
  };

  const startFrom = (fresh: AccessKey) => {
    const next = valuesFromUser(fresh);
    loaded.current = { session, baseline: serializeDraft(next) };
    reset(next);
  };

  const showServerError = (err: unknown) => {
    if (!(err instanceof ApiError) || Object.keys(err.fields).length === 0) {
      setError('root', { message: errText(err, t('common.error_generic')) });
      return;
    }
    const unmatched: string[] = [];
    const routed: string[] = [];
    for (const [field, message] of Object.entries(err.fields)) {
      const name = field.split('.')[0];
      if (!(SERVER_FIELDS as readonly string[]).includes(name)) {
        unmatched.push(`${field}: ${message}`);
        continue;
      }
      const text = name === 'sub_slug' && message === 'taken' ? t('users.validation_slug_taken') : message;
      setError(name as ServerField, { type: 'server', message: text });
      routed.push(name);
    }
    if (unmatched.length > 0) setError('root', { message: unmatched.join('; ') });
    routeErrors(routed);
  };

  const onSubmit = async (v: UserFormValues) => {
    if (!user) return;
    try {
      await patchKey.mutateAsync(patchPayload(v));
    } catch (err) {
      showServerError(err);
      return;
    }
    try {
      const before = new Set(user.nodes.map((n) => n.node_id));
      const after = new Set(v.node_ids);
      for (const nodeId of v.node_ids) if (!before.has(nodeId)) await bindKey.mutateAsync(nodeId);
      for (const nodeId of before) if (!after.has(nodeId)) await unbindKey.mutateAsync(nodeId);
    } catch (err) {
      const fresh = await userQuery.refetch();
      if (fresh.data) startFrom(fresh.data);
      setValue('node_ids', v.node_ids, { shouldDirty: true });
      setError('node_ids', { type: 'server', message: errText(err, t('common.error_generic')) });
      routeErrors(['node_ids']);
      return;
    }
    draft.clear();
    const fresh = await userQuery.refetch();
    if (fresh.data) startFrom(fresh.data);
    toast.add({ description: t('users.saved'), type: 'success' });
  };

  const run = async (action: () => Promise<unknown>, success: string) => {
    try {
      await action();
      toast.add({ description: success, type: 'success' });
    } catch (err) {
      toast.add({ description: errText(err, t('common.error_generic')), type: 'error' });
    }
  };

  const close = () => onOpenChange(false);

  const confirmSpec = user
    ? {
        rotate: {
          title: t('users.rotate_title', { label: user.label }),
          description: t('users.rotate_description'),
          label: t('keys.action_rotate'),
          destructive: false,
          action: () => run(() => rotateKey.mutateAsync(user.id), t('keys.rotate_success')),
        },
        reissue: {
          title: t('keys.subscription_rotate_confirm_title'),
          description: t('keys.subscription_rotate_confirm_description'),
          label: t('users.subscription_reissue'),
          destructive: false,
          action: () => run(() => issueLink.mutateAsync(), t('keys.subscription_rotate_success')),
        },
        unlink: {
          title: t('users.unlink_title'),
          description: t('users.unlink_description'),
          label: t('keys.subscription_revoke'),
          destructive: true,
          action: () => run(() => revokeLink.mutateAsync(), t('keys.subscription_revoke_success')),
        },
        revoke: {
          title: t('users.revoke_title', { label: user.label }),
          description: user.type === 'SHARED' ? t('keys.revoke_confirm_shared') : t('users.revoke_description'),
          label: t('users.revoke'),
          destructive: true,
          action: () => run(() => revokeKey.mutateAsync(user.id), t('users.revoked')),
        },
        delete: {
          title: t('users.delete_title', { label: user.label }),
          description: t('users.delete_description'),
          label: t('common.delete'),
          destructive: true,
          action: async () => {
            await run(() => deleteKey.mutateAsync(user.id), t('users.deleted'));
            close();
          },
        },
      }
    : null;
  const active = confirm && confirmSpec ? confirmSpec[confirm] : null;

  return (
    <>
      <Dialog open={open && !!keyId} onOpenChange={onOpenChange}>
        <DialogContent className={WINDOW_CLASS}>
          <DialogHeader className="gap-3 px-4 pt-4 sm:px-6 sm:pt-5">
            <div className="flex min-w-0 items-center gap-2">
              <DialogTitle className="truncate">{user ? user.label : t('users.loading')}</DialogTitle>
              <HelpButton topic="keys.detail" className="-my-1" />
            </div>
            {user && (
              <DialogDescription className="flex flex-wrap items-center gap-x-4 gap-y-1 text-label text-mute">
                <StateBadge state={user.state} />
                <Badge>{t(user.type === 'SHARED' ? 'keys.type_shared' : 'keys.type_personal')}</Badge>
                {user.owner_label && <span className="max-w-full truncate">{user.owner_label}</span>}
                <UserPresence user={user} />
              </DialogDescription>
            )}
          </DialogHeader>

          <Tabs value={tab} onValueChange={(v) => setTab(v as UserTab)} className="mt-3 min-h-0 flex-1 gap-0">
            <div className="shrink-0 overflow-x-auto px-4 [scrollbar-width:none] sm:px-6">
              <TabsList className="w-max min-w-full gap-5">
                {USER_TABS.map((value) => (
                  <TabsTrigger key={value} value={value}>
                    {t(TAB_LABEL[value])}
                    {errorTabs.has(value) && (
                      <>
                        <CircleAlert className="size-3.5 text-destructive" aria-hidden="true" />
                        <span className="sr-only">{t('users.tab_has_errors')}</span>
                      </>
                    )}
                  </TabsTrigger>
                ))}
              </TabsList>
            </div>
            <div ref={bodyRef} className="min-h-0 flex-1 overflow-y-auto px-4 py-5 sm:px-6">
              {!user ? (
                <div className="space-y-4">
                  <Skeleton className="h-16 w-full" />
                  <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                    {[0, 1, 2, 3].map((i) => (
                      <Skeleton key={i} className="h-18 w-full" />
                    ))}
                  </div>
                  <Skeleton className="h-24 w-full" />
                </div>
              ) : (
                <>
                  {draft.draft && (
                    <DraftBanner className="mb-5" savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />
                  )}
                  <form id={FORM_ID} onSubmit={(e) => void handleSubmit(onSubmit, (errs) => routeErrors(Object.keys(errs)))(e)} noValidate>
                    <TabsContent value="main" className="space-y-5">
                      {welcome && !revoked && (
                        <Notice tone="welcome">{t(user.type === 'SHARED' ? 'users.welcome_shared' : 'users.welcome')}</Notice>
                      )}
                      {user.state === 'pending' && <Notice tone="warn">{t('keys.link_pending_hint')}</Notice>}
                      {user.state === 'disabled' && <Notice tone="mute">{t('users.disabled_notice')}</Notice>}
                      {user.state === 'expired' && <Notice tone="warn">{t('users.expired_notice')}</Notice>}
                      {revoked && (
                        <Notice tone="err" role="alert">
                          {t('users.revoked_notice')}
                        </Notice>
                      )}
                      {!revoked && (
                        <AccessSwitch
                          user={user}
                          disabled={!isWriter || setDisabled.isPending}
                          onToggle={(on) =>
                            void run(() => setDisabled.mutateAsync({ id: user.id, disabled: !on }), on ? t('users.enabled') : t('users.disabled'))
                          }
                        />
                      )}
                      <UserFacts user={user} measured={measured} nodesTotal={nodes.length} />
                      {!revoked && (
                        <SubscriptionBlock
                          user={user}
                          canIssue={isWriter}
                          onShowQr={() => setQrOpen(true)}
                          onIssueLink={() =>
                            user.subscription_legacy
                              ? setConfirm('reissue')
                              : void run(() => issueLink.mutateAsync(), t('keys.subscription_create_success'))
                          }
                        />
                      )}
                      {!revoked && <DirectLinksRow count={linksCount} onOpen={() => setLinksOpen(true)} />}
                      <section className="space-y-4 border-t border-hairline pt-5" aria-labelledby="user-about-title">
                        <h3 id="user-about-title" className="text-body font-semibold">
                          {t('users.card_about')}
                        </h3>
                        <div className="grid gap-4 sm:grid-cols-2">
                          <AboutFields
                            control={control}
                            register={register}
                            errors={errors}
                            values={values}
                            locked={locked}
                            subscriptionBase={subscriptionBase(serviceQuery.data)}
                          />
                        </div>
                        <ExpiryField control={control} register={register} errors={errors} values={values} locked={locked} setValue={setValue} />
                      </section>
                      <p className="mono text-mono text-mute">{t('users.created_at', { date: formatDateTime(user.created_at, i18n.language) })}</p>
                    </TabsContent>
                    <TabsContent value="access" className="space-y-5">
                      <section className="space-y-2" aria-labelledby="user-type-title">
                        <h3 id="user-type-title" className="text-body font-semibold">
                          {t('users.type_label')}
                        </h3>
                        <div className="flex items-start gap-3 rounded-surface border border-hairline px-4 py-3">
                          <Badge className="mt-0.5">{t(user.type === 'SHARED' ? 'keys.type_shared' : 'keys.type_personal')}</Badge>
                          <div className="min-w-0 space-y-0.5">
                            <p className="text-body">{t(user.type === 'SHARED' ? 'users.create_hint_shared' : 'users.create_hint_personal')}</p>
                            <p className="text-label text-mute">{t('users.type_fixed')}</p>
                          </div>
                        </div>
                      </section>
                      <ServersField control={control} errors={errors} locked={locked} nodes={nodes} scope={scope} />
                    </TabsContent>
                    <TabsContent value="limits" className="space-y-4">
                      <div className="flex items-center gap-2">
                        <h3 className="text-body font-semibold">{t('users.card_limits')}</h3>
                        <HelpButton topic="keys.limits" className="-my-1" />
                      </div>
                      <LimitsCardBody control={control} errors={errors} locked={locked} scope={scope} />
                    </TabsContent>
                  </form>
                  <TabsContent value="stats" className="space-y-4">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <h3 className="text-body font-semibold">{t('keys.stats_title')}</h3>
                      {measured && (
                        <SegmentedControl
                          label={t('keys.stats_range_label')}
                          value={statsRange}
                          onChange={setStatsRange}
                          options={STATS_RANGES.map((r) => ({ value: r, label: t(`keys.stats_range_${r}`) }))}
                        />
                      )}
                    </div>
                    <KeyStatsSection keyId={user.id} hasTelemtNode={measured} range={statsRange} />
                  </TabsContent>
                </>
              )}
            </div>
          </Tabs>

          <DialogFooter className="static m-0 shrink-0 flex-col gap-2 rounded-none px-4 py-3 sm:flex-col sm:px-6">
            {errors.root && (
              <p role="alert" className="flex items-start gap-2 text-body text-destructive">
                <span className="mt-2 size-[7px] shrink-0 rounded-pill bg-destructive" aria-hidden="true" />
                {errors.root.message}
              </p>
            )}
            <div className="flex items-center gap-2">
              {isWriter && user && (
                <DropdownMenu>
                  <DropdownMenuTrigger render={<Button type="button" variant="outline" />}>
                    <MoreHorizontal />
                    <span className="sr-only sm:not-sr-only">{t('users.more_actions')}</span>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start" side="top" className="min-w-64">
                    {!revoked && (
                      <DropdownMenuGroup>
                        <DropdownMenuLabel>{t('users.menu_manage')}</DropdownMenuLabel>
                        <DropdownMenuItem
                          onClick={() =>
                            void run(
                              () => setDisabled.mutateAsync({ id: user.id, disabled: user.state !== 'disabled' }),
                              user.state === 'disabled' ? t('users.enabled') : t('users.disabled'),
                            )
                          }
                        >
                          <Power />
                          {user.state === 'disabled' ? t('users.enable') : t('users.disable')}
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => setConfirm('rotate')}>
                          <RotateCw />
                          {t('keys.action_rotate')}
                        </DropdownMenuItem>
                        {user.subscription_active ? (
                          <>
                            <DropdownMenuItem onClick={() => setConfirm('reissue')}>
                              <RefreshCw />
                              {t('users.subscription_reissue')}
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => setConfirm('unlink')}>
                              <Ban />
                              {t('keys.subscription_revoke')}
                            </DropdownMenuItem>
                          </>
                        ) : (
                          <DropdownMenuItem onClick={() => void run(() => issueLink.mutateAsync(), t('keys.subscription_create_success'))}>
                            <Plus />
                            {t('keys.subscription_create')}
                          </DropdownMenuItem>
                        )}
                      </DropdownMenuGroup>
                    )}
                    {!revoked && <DropdownMenuSeparator />}
                    <DropdownMenuGroup>
                      <DropdownMenuLabel>{t('keys.danger_zone')}</DropdownMenuLabel>
                      {!revoked && (
                        <DropdownMenuItem variant="destructive" onClick={() => setConfirm('revoke')}>
                          <Undo2 />
                          {t('users.revoke')}
                        </DropdownMenuItem>
                      )}
                      <DropdownMenuItem variant="destructive" onClick={() => setConfirm('delete')}>
                        <Trash2 />
                        {t('common.delete')}
                      </DropdownMenuItem>
                    </DropdownMenuGroup>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
              <span className="flex-1" />
              {isDirty && !revoked && <span className="hidden text-label text-warn sm:inline">{t('users.unsaved')}</span>}
              <Button type="button" variant="outline" onClick={close}>
                {t('common.close')}
              </Button>
              {isWriter && !revoked && user && (
                <Button type="submit" form={FORM_ID} disabled={isSubmitting || !isDirty}>
                  {t('common.save')}
                </Button>
              )}
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {user && (
        <>
          <KeyLinkDialog open={linksOpen} onOpenChange={setLinksOpen} keyId={user.id} />

          <Dialog open={qrOpen} onOpenChange={setQrOpen}>
            <DialogContent className="sm:max-w-sm">
              <DialogHeader>
                <DialogTitle>{t('keys.subscription_qr_alt')}</DialogTitle>
                <DialogDescription>{t('users.qr_hint')}</DialogDescription>
              </DialogHeader>
              <img
                src={subscriptionQrUrl(user.id, !!user.subscription_short_url)}
                alt={t('keys.subscription_qr_alt')}
                width={320}
                height={320}
                className="mx-auto size-72 rounded-surface bg-white p-3"
              />
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  nativeButton={false}
                  render={<a href={subscriptionQrUrl(user.id, !!user.subscription_short_url, 640)} download={`${user.label}-subscription.png`} />}
                >
                  <Download />
                  {t('keys.link_download_qr')}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>

          {active && (
            <ConfirmDialog
              open={!!confirm}
              onOpenChange={(o) => !o && setConfirm(null)}
              title={active.title}
              description={active.description}
              destructive={active.destructive}
              confirmLabel={active.label}
              onConfirm={active.action}
            />
          )}
        </>
      )}
    </>
  );
}
