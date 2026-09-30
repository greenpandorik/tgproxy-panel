import { zodResolver } from '@hookform/resolvers/zod';
import {
  Ban,
  CalendarClock,
  ChartLine,
  ExternalLink,
  Gauge,
  Link2,
  MoreHorizontal,
  Power,
  QrCode,
  RefreshCw,
  RotateCw,
  Trash2,
  Undo2,
  UserRound,
} from 'lucide-react';
import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';

import {
  subscriptionQrUrl,
  useBindKey,
  useCreateSubscription,
  useDeleteKey,
  useKey,
  usePatchKey,
  useRevokeKey,
  useRevokeSubscription,
  useRotateKey,
  useSetKeyDisabled,
  useUnbindKey,
} from '@/api/keys';
import { useNodes } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { CopyButton } from '@/components/common/CopyButton';
import { DraftBanner } from '@/components/common/DraftBanner';
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
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';
import { formatBytes, formatDateTime, formatRelativeTime } from '@/lib/format';
import { cn } from '@/lib/utils';

import { KeyLinkDialog } from './KeyLinkDialog';
import { KeyStatsSection } from './KeyStatsSection';
import { transportScope } from './transport';
import { AboutFields, AccessFields, LimitsCardBody, UserCard } from './UserCards';
import { StateBadge } from './StateBadge';
import { TONE_TEXT, expiryTone } from './userState';
import { patchPayload, userSchema, valuesFromUser, NEW_USER } from './userForm';

import type { UserFormValues } from './userForm';
import type { AccessKey } from '@/api/types';
import type { ReactNode } from 'react';

const FORM_ID = 'user-form';

function errText(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback;
}

function Tile({ label, value, sub, tone }: { label: string; value: ReactNode; sub?: ReactNode; tone?: string }) {
  return (
    <div className="min-w-0 rounded-control border border-hairline bg-surface px-3 py-2">
      <p className="truncate text-label text-mute">{label}</p>
      <p className={cn('truncate text-body font-semibold', tone)}>{value}</p>
      {sub && <p className="truncate text-label text-mute">{sub}</p>}
    </div>
  );
}

function OverviewCard({
  user,
  measured,
  onShowLinks,
  onShowStats,
  onShowQr,
  onIssueLink,
}: {
  user: AccessKey;
  measured: boolean;
  onShowLinks: () => void;
  onShowStats: () => void;
  onShowQr: () => void;
  onIssueLink: () => void;
}) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const revoked = user.state === 'revoked';
  const link = user.subscription_short_url ?? user.subscription_url;
  const tone = expiryTone(user.expires_at);

  const online =
    user.live.connections > 0
      ? { value: t('users.online_now', { count: user.live.connections }), sub: t('users.online_ips', { count: user.live.ips }), tone: 'text-ok' }
      : user.last_seen_at
        ? { value: t('users.seen_ago', { ago: formatRelativeTime(user.last_seen_at, lang) }), sub: formatDateTime(user.last_seen_at, lang) }
        : { value: t('users.never_seen'), sub: undefined };

  return (
    <UserCard icon={UserRound} title={t('users.card_overview')}>
      <div className="grid grid-cols-2 gap-2">
        <Tile
          label={t('users.field_expires')}
          value={user.expires_at ? formatRelativeTime(user.expires_at, lang) : t('keys.no_expiry')}
          sub={user.expires_at ? formatDateTime(user.expires_at, lang) : undefined}
          tone={user.expires_at ? TONE_TEXT[tone] : undefined}
        />
        <Tile
          label={t('keys.column_traffic')}
          value={measured ? formatBytes(user.traffic_30d) : '—'}
          sub={measured ? undefined : t('users.traffic_unmeasured')}
        />
        <Tile label={t('users.column_online')} value={online.value} sub={online.sub} tone={online.tone} />
        <Tile
          label={t('keys.column_nodes')}
          value={user.nodes.length}
          sub={user.nodes.map((n) => n.node_name).join(', ') || undefined}
        />
      </div>

      {!revoked && (
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <p className="text-label font-medium text-foreground">{t('keys.subscription_title')}</p>
            <HelpButton topic="keys.link" className="-my-1" />
          </div>
          {link ? (
            <>
              <div className="flex items-center gap-2">
                <Input readOnly value={link} className="mono text-mono" onFocus={(e) => e.target.select()} aria-label={t('keys.subscription_title')} />
                <CopyButton
                  value={link}
                  label={t('keys.subscription_copy')}
                  className="size-9 shrink-0 border-hairline-strong bg-surface text-foreground hover:bg-elevated"
                />
              </div>
              {user.subscription_short_url && user.subscription_url && (
                <p className="mono truncate text-mono text-mute" title={user.subscription_url}>
                  {t('users.full_link')}: {user.subscription_url}
                </p>
              )}
              <div className="flex flex-wrap gap-2">
                <Button type="button" variant="outline" size="sm" onClick={onShowQr}>
                  <QrCode />
                  {t('users.show_qr')}
                </Button>
                <Button type="button" variant="outline" size="sm" nativeButton={false} render={<a href={link} target="_blank" rel="noopener noreferrer" />}>
                  <ExternalLink />
                  {t('users.open_page')}
                </Button>
              </div>
            </>
          ) : (
            <div className="space-y-2 rounded-control border border-dashed border-hairline-strong p-3">
              <p className="text-label text-mute">
                {user.subscription_legacy ? t('users.subscription_legacy') : t('users.subscription_none')}
              </p>
              <Button type="button" size="sm" onClick={onIssueLink}>
                <RefreshCw />
                {user.subscription_legacy ? t('users.subscription_reissue') : t('keys.subscription_create')}
              </Button>
            </div>
          )}
        </div>
      )}

      <div className="flex flex-wrap gap-2 border-t border-hairline pt-3">
        {!revoked && (
          <Button type="button" variant="ghost" size="sm" onClick={onShowLinks}>
            <Link2 />
            {t('users.direct_links')}
          </Button>
        )}
        <Button type="button" variant="ghost" size="sm" onClick={onShowStats}>
          <ChartLine />
          {t('keys.stats_title')}
        </Button>
      </div>
    </UserCard>
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
  const revokeLink = useRevokeSubscription(id);

  const [linksOpen, setLinksOpen] = useState(false);
  const [statsOpen, setStatsOpen] = useState(false);
  const [qrOpen, setQrOpen] = useState(false);
  const [confirm, setConfirm] = useState<null | 'rotate' | 'reissue' | 'unlink' | 'revoke' | 'delete'>(null);

  const {
    control,
    register,
    handleSubmit,
    reset,
    watch,
    setValue,
    setError,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<UserFormValues>({ resolver: zodResolver(userSchema), defaultValues: { ...NEW_USER, creating: false } });

  useEffect(() => {
    if (user) reset(valuesFromUser(user));
  }, [user, reset]);

  const values = watch();
  const revoked = user?.state === 'revoked';
  const locked = revoked || !isWriter;
  const nodes = nodesQuery.data?.items ?? [];
  const scope = transportScope(nodes, values.node_ids);
  const boundIds = new Set((user?.nodes ?? []).map((n) => n.node_id));
  const measured = nodes.some((n) => n.engine === 'telemt' && boundIds.has(n.id));

  const draft = useDraft<UserFormValues>(`user-edit-${id || 'none'}`, values, {
    initial: user ? valuesFromUser(user) : NEW_USER,
    open: open && !!user && !locked,
  });

  const resumeDraft = () => {
    if (!draft.draft) return;
    reset(draft.draft.value, { keepDefaultValues: true });
    draft.dismiss();
  };

  const onSubmit = async (v: UserFormValues) => {
    if (!user) return;
    try {
      await patchKey.mutateAsync(patchPayload(v));
      const before = new Set(user.nodes.map((n) => n.node_id));
      const after = new Set(v.node_ids);
      for (const nodeId of v.node_ids) if (!before.has(nodeId)) await bindKey.mutateAsync(nodeId);
      for (const nodeId of before) if (!after.has(nodeId)) await unbindKey.mutateAsync(nodeId);
      draft.clear();
      await userQuery.refetch();
      toast.add({ description: t('users.saved'), type: 'success' });
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        const known = ['label', 'owner_label', 'carrier_mode', 'expires_at', 'sub_slug', 'node_ids'] as const;
        const unmatched: string[] = [];
        for (const field of Object.keys(err.fields)) {
          if ((known as readonly string[]).includes(field)) {
            const msg = field === 'sub_slug' && err.fields[field] === 'taken' ? t('users.validation_slug_taken') : err.fields[field];
            setError(field as (typeof known)[number], { message: msg });
          } else {
            unmatched.push(`${field}: ${err.fields[field]}`);
          }
        }
        if (unmatched.length > 0) setError('root', { message: unmatched.join('; ') });
        return;
      }
      setError('root', { message: errText(err, t('common.error_generic')) });
    }
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
          label: t('users.subscription_new'),
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
        <DialogContent className="max-h-[94vh] overflow-y-auto sm:max-w-5xl">
          <DialogHeader>
            <div className="flex items-center gap-2 pr-8">
              <DialogTitle className="truncate">{user ? user.label : t('users.loading')}</DialogTitle>
              <HelpButton topic="keys.detail" className="-my-1" />
            </div>
            {user && (
              <DialogDescription className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <StateBadge state={user.state} />
                <Badge>{t(user.type === 'SHARED' ? 'keys.type_shared' : 'keys.type_personal')}</Badge>
                {user.owner_label && <span className="truncate text-mute">{user.owner_label}</span>}
                <span className="mono text-mono text-mute">
                  {t('users.created_at', { date: formatDateTime(user.created_at, i18n.language) })}
                </span>
              </DialogDescription>
            )}
          </DialogHeader>

          {!user ? (
            <div className="grid gap-4 lg:grid-cols-2">
              {[0, 1].map((i) => (
                <div key={i} className="space-y-3">
                  <Skeleton className="h-40 w-full" />
                  <Skeleton className="h-56 w-full" />
                </div>
              ))}
            </div>
          ) : (
            <>
              {welcome && !revoked && (
                <p className="flex items-start gap-2 rounded-control bg-elevated px-3 py-2 text-label text-foreground">
                  <span className="mt-1 size-[7px] shrink-0 rounded-pill bg-ok" aria-hidden="true" />
                  {t('users.welcome')}
                </p>
              )}
              {user.state === 'pending' && (
                <p className="flex items-start gap-2 text-label text-warn">
                  <span className="mt-1 size-[7px] shrink-0 rounded-pill bg-warn" aria-hidden="true" />
                  {t('keys.link_pending_hint')}
                </p>
              )}
              {user.state === 'disabled' && (
                <p className="flex items-start gap-2 text-label text-mute">
                  <span className="mt-1 size-[7px] shrink-0 rounded-pill bg-dim" aria-hidden="true" />
                  {t('users.disabled_notice')}
                </p>
              )}
              {user.state === 'expired' && (
                <p className="flex items-start gap-2 text-label text-warn">
                  <span className="mt-1 size-[7px] shrink-0 rounded-pill bg-warn" aria-hidden="true" />
                  {t('users.expired_notice')}
                </p>
              )}
              {revoked && (
                <p role="alert" className="flex items-start gap-2 text-label text-destructive">
                  <span className="mt-1 size-[7px] shrink-0 rounded-pill bg-destructive" aria-hidden="true" />
                  {t('users.revoked_notice')}
                </p>
              )}
              {draft.draft && <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />}

              <form
                id={FORM_ID}
                className="grid items-start gap-4 lg:grid-cols-2"
                onSubmit={(e) => void handleSubmit(onSubmit)(e)}
                noValidate
              >
                <div className="flex min-w-0 flex-col gap-4">
                  <OverviewCard
                    user={user}
                    measured={measured}
                    onShowLinks={() => setLinksOpen(true)}
                    onShowStats={() => setStatsOpen(true)}
                    onShowQr={() => setQrOpen(true)}
                    onIssueLink={() => void run(() => issueLink.mutateAsync(), t('keys.subscription_create_success'))}
                  />
                  <UserCard icon={UserRound} title={t('users.card_about')}>
                    <AboutFields
                      control={control}
                      register={register}
                      errors={errors}
                      values={values}
                      locked={locked}
                      subscriptionBase={user.subscription_url?.split('/s/')[0] ?? window.location.origin}
                    />
                  </UserCard>
                </div>
                <div className="flex min-w-0 flex-col gap-4">
                  <UserCard icon={CalendarClock} title={t('users.card_access')}>
                    <AccessFields
                      control={control}
                      register={register}
                      errors={errors}
                      values={values}
                      locked={locked}
                      setValue={setValue}
                      nodes={nodes}
                      scope={scope}
                    />
                  </UserCard>
                  <UserCard icon={Gauge} title={t('users.card_limits')} actions={<HelpButton topic="keys.limits" />}>
                    <LimitsCardBody control={control} errors={errors} locked={locked} scope={scope} />
                  </UserCard>
                </div>
              </form>

              {errors.root && (
                <p role="alert" className="flex items-start gap-2 text-body text-destructive">
                  <span className="mt-2 size-[7px] shrink-0 rounded-pill bg-destructive" aria-hidden="true" />
                  {errors.root.message}
                </p>
              )}

              <DialogFooter className="sm:justify-between">
                {isWriter ? (
                  <DropdownMenu>
                    <DropdownMenuTrigger render={<Button type="button" variant="outline" />}>
                      <MoreHorizontal />
                      {t('users.more_actions')}
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="start" className="min-w-64">
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
                                {t('users.subscription_new')}
                              </DropdownMenuItem>
                              <DropdownMenuItem onClick={() => setConfirm('unlink')}>
                                <Ban />
                                {t('keys.subscription_revoke')}
                              </DropdownMenuItem>
                            </>
                          ) : (
                            <DropdownMenuItem
                              onClick={() => void run(() => issueLink.mutateAsync(), t('keys.subscription_create_success'))}
                            >
                              <RefreshCw />
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
                ) : (
                  <span />
                )}
                <div className="flex flex-wrap justify-end gap-2">
                  <Button type="button" variant="outline" onClick={close}>
                    {t('common.close')}
                  </Button>
                  {isWriter && !revoked && (
                    <Button type="submit" form={FORM_ID} disabled={isSubmitting || !isDirty}>
                      {t('common.save')}
                    </Button>
                  )}
                </div>
              </DialogFooter>
            </>
          )}
        </DialogContent>
      </Dialog>

      {user && (
        <>
          <KeyLinkDialog open={linksOpen} onOpenChange={setLinksOpen} keyId={user.id} />

          <Dialog open={statsOpen} onOpenChange={setStatsOpen}>
            <DialogContent className="sm:max-w-2xl">
              <DialogHeader>
                <DialogTitle>{t('users.stats_title', { label: user.label })}</DialogTitle>
              </DialogHeader>
              <KeyStatsSection keyId={user.id} hasTelemtNode={measured} />
            </DialogContent>
          </Dialog>

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
