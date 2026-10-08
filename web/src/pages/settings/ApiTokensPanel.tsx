import { AlertTriangle, ExternalLink, KeyRound, Plus } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useApiTokens, useApiTokenScopes, useCreateApiToken, useRevokeApiToken } from '@/api/apiTokens';
import type { ApiToken, ApiTokenScopes } from '@/api/apiTokens';
import { useAuth } from '@/auth/AuthProvider';
import { CopyButton } from '@/components/common/CopyButton';
import { EmptyState } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel } from '@/components/common/Panel';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { ApiError } from '@/lib/api';
import { formatDateTime, formatRelativeTime } from '@/lib/format';

function tokenStatus(token: ApiToken) {
  if (token.revoked_at) return 'revoked';
  return new Date(token.expires_at).getTime() <= Date.now() ? 'expired' : 'active';
}

function CreateTokenDialog({ catalog, isWriter, onClose }: { catalog: ApiTokenScopes; isWriter: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const available = catalog.scopes.filter((scope) => isWriter || scope.action === 'read');
  const maxDays = Math.min(365, catalog.max_expires_in_days);
  const [name, setName] = useState('');
  const [days, setDays] = useState(String(Math.min(30, maxDays)));
  const [scopes, setScopes] = useState<string[]>(() =>
    available.filter((scope) => scope.action === 'read').map((scope) => scope.id),
  );
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [secret, setSecret] = useState<string | null>(null);
  const create = useCreateApiToken(setSecret);
  const resources = [...new Set(available.map((scope) => scope.resource))];
  const readScopes = available.filter((scope) => scope.action === 'read').map((scope) => scope.id);
  const isReadOnly = scopes.length === readScopes.length && readScopes.every((id) => scopes.includes(id));
  const isFull = scopes.length === available.length && available.every((scope) => scopes.includes(scope.id));
  const close = () => {
    if (create.isPending) return;
    setSecret(null);
    create.reset();
    onClose();
  };
  const submit = async () => {
    const trimmed = name.trim();
    const next: Record<string, string> = {};
    if ([...trimmed].length < 1 || [...trimmed].length > 80) next.name = t('api_tokens.name_error');
    const duration = Number(days);
    if (!Number.isInteger(duration) || duration < 1 || duration > maxDays)
      next.expires_in_days = t('api_tokens.days_error', { max: maxDays });
    const allowed = scopes.filter((id) => available.some((scope) => scope.id === id));
    if (!allowed.length) next.scopes = t('api_tokens.scopes_error');
    setErrors(next);
    if (Object.keys(next).length) return;
    try {
      await create.mutateAsync({ name: trimmed, expires_in_days: duration, scopes: allowed });
    } catch (error) {
      setErrors(error instanceof ApiError ? { ...error.fields, root: error.message } : { root: t('common.error_generic') });
    }
  };
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent className="sm:max-w-xl" showCloseButton={!create.isPending}>
        <DialogHeader>
          <DialogTitle>{t(secret ? 'api_tokens.secret_title' : 'api_tokens.create_title')}</DialogTitle>
          <DialogDescription>{t(secret ? 'api_tokens.secret_description' : 'api_tokens.create_description')}</DialogDescription>
        </DialogHeader>
        {secret ? (
          <>
            <p className="flex items-start gap-2 rounded-control border border-warn/35 bg-warn/10 p-3 text-body text-warn">
              <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
              {t('api_tokens.secret_warning')}
            </p>
            <pre
              aria-label={t('api_tokens.secret_label')}
              className="mono select-all rounded-control border border-hairline-strong bg-background p-3 text-mono break-all whitespace-pre-wrap"
            >
              {secret}
            </pre>
            <DialogFooter>
              <CopyButton value={secret} label={t('api_tokens.copy')} showLabel />
              <Button type="button" onClick={close}>
                {t('api_tokens.done')}
              </Button>
            </DialogFooter>
          </>
        ) : (
          <form
            className="space-y-4"
            noValidate
            onSubmit={(event) => {
              event.preventDefault();
              void submit();
            }}
          >
            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_150px]">
              <div className="space-y-2">
                <Label htmlFor="api-token-name">{t('api_tokens.name')}</Label>
                <Input
                  id="api-token-name"
                  autoComplete="off"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  disabled={create.isPending}
                  aria-invalid={!!errors.name}
                  aria-describedby={errors.name ? 'api-token-name-error' : undefined}
                />
                {errors.name && (
                  <p id="api-token-name-error" className="text-label text-destructive">
                    {errors.name}
                  </p>
                )}
              </div>
              <div className="space-y-2">
                <Label htmlFor="api-token-days">{t('api_tokens.days')}</Label>
                <Input
                  id="api-token-days"
                  type="number"
                  min={1}
                  max={maxDays}
                  step={1}
                  value={days}
                  onChange={(event) => setDays(event.target.value)}
                  disabled={create.isPending}
                  aria-invalid={!!errors.expires_in_days}
                  aria-describedby="api-token-days-hint"
                />
                <p
                  id="api-token-days-hint"
                  className={errors.expires_in_days ? 'text-label text-destructive' : 'text-label text-mute'}
                >
                  {errors.expires_in_days ?? t('api_tokens.days_hint', { max: maxDays })}
                </p>
              </div>
            </div>
            <fieldset className="space-y-3" disabled={create.isPending} aria-describedby="api-token-permissions-hint">
              <legend className="mb-2 text-body font-medium">{t('api_tokens.permissions')}</legend>
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  variant={isReadOnly ? 'secondary' : 'outline'}
                  size="sm"
                  aria-pressed={isReadOnly}
                  onClick={() => setScopes(readScopes)}
                >
                  {t('api_tokens.read_only')}
                </Button>
                {isWriter && (
                  <Button
                    type="button"
                    variant={isFull ? 'secondary' : 'outline'}
                    size="sm"
                    aria-pressed={isFull}
                    onClick={() => setScopes(available.map((scope) => scope.id))}
                  >
                    {t('api_tokens.full_access')}
                  </Button>
                )}
              </div>
              <div className="divide-y divide-hairline rounded-control border border-hairline-strong">
                {resources.map((resource) => (
                  <div key={resource} className="flex flex-wrap items-center justify-between gap-3 px-3 py-2.5">
                    <span className="text-body font-medium">
                      {t(`api_tokens.resources.${resource}`, { defaultValue: resource })}
                    </span>
                    <div className="flex flex-wrap gap-4">
                      {available
                        .filter((scope) => scope.resource === resource)
                        .map((scope) => (
                          <label key={scope.id} className="flex cursor-pointer items-center gap-2 text-label text-mute">
                            <Checkbox
                              checked={scopes.includes(scope.id)}
                              aria-label={`${t(`api_tokens.resources.${resource}`, { defaultValue: resource })}: ${t(`api_tokens.${scope.action}`)}`}
                              onCheckedChange={(checked) =>
                                setScopes((current) =>
                                  checked ? [...current, scope.id] : current.filter((id) => id !== scope.id),
                                )
                              }
                            />
                            {t(`api_tokens.${scope.action}`)}
                          </label>
                        ))}
                    </div>
                  </div>
                ))}
              </div>
              {errors.scopes && <p className="text-label text-destructive">{errors.scopes}</p>}
              <p id="api-token-permissions-hint" className="text-label text-mute">
                {t('api_tokens.permissions_hint')}
              </p>
            </fieldset>
            {errors.root && (
              <p
                role="alert"
                className="rounded-control border border-destructive/30 bg-destructive/10 px-3 py-2 text-body text-destructive"
              >
                {errors.root}
              </p>
            )}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={close} disabled={create.isPending}>
                {t('common.cancel')}
              </Button>
              <Button type="submit" disabled={create.isPending}>
                {t('api_tokens.create')}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function TokenRow({ token, onRevoke }: { token: ApiToken; onRevoke: () => void }) {
  const { t, i18n } = useTranslation();
  const status = tokenStatus(token);
  return (
    <li className="grid min-w-0 gap-4 px-4 py-4 md:grid-cols-[minmax(0,1.15fr)_minmax(0,1.5fr)_minmax(0,1fr)_auto] md:items-start">
      <div className="min-w-0 space-y-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="break-all text-body font-medium">{token.name}</span>
          <Badge
            className={
              status === 'active' ? 'border-ok/30 text-ok' : status === 'expired' ? 'border-warn/30 text-warn' : 'text-mute'
            }
          >
            {t(`api_tokens.${status}`)}
          </Badge>
        </div>
        <p className="mono break-all text-mono text-mute">{token.prefix}…</p>
        <p className="text-label text-mute">
          {t('api_tokens.created', { time: formatDateTime(token.created_at, i18n.language) })}
        </p>
      </div>
      <div className="min-w-0">
        <p className="mb-1 text-label text-mute md:hidden">{t('api_tokens.permissions')}</p>
        <ul className="flex flex-wrap gap-1.5" aria-label={t('api_tokens.token_permissions', { name: token.name })}>
          {token.scopes.map((scope) => {
            const [resource, action] = scope.split(':');
            return (
              <li key={scope}>
                <Badge className="whitespace-normal">
                  {t(`api_tokens.resources.${resource}`, { defaultValue: resource })}:{' '}
                  {t(`api_tokens.${action}`, { defaultValue: action })}
                </Badge>
              </li>
            );
          })}
        </ul>
      </div>
      <dl className="grid gap-2 text-label">
        <div>
          <dt className="text-mute">{t('api_tokens.expires')}</dt>
          <dd>
            <time dateTime={token.expires_at}>{formatDateTime(token.expires_at, i18n.language)}</time>
          </dd>
        </div>
        <div>
          <dt className="text-mute">{t('api_tokens.last_use')}</dt>
          <dd>
            {token.last_used_at ? (
              <time dateTime={token.last_used_at} title={formatDateTime(token.last_used_at, i18n.language)}>
                {formatRelativeTime(token.last_used_at, i18n.language)}
              </time>
            ) : (
              t('api_tokens.never_used')
            )}
          </dd>
        </div>
      </dl>
      <div>
        {!token.revoked_at && (
          <Button
            type="button"
            variant="destructive"
            size="sm"
            onClick={onRevoke}
            aria-label={t('api_tokens.revoke_named', { name: token.name })}
          >
            {t('api_tokens.revoke')}
          </Button>
        )}
      </div>
    </li>
  );
}

export function ApiTokensPanel() {
  const { t, i18n } = useTranslation();
  const { isWriter, loading } = useAuth();
  const tokens = useApiTokens();
  const scopes = useApiTokenScopes();
  const revoke = useRevokeApiToken();
  const [createOpen, setCreateOpen] = useState(false);
  const [target, setTarget] = useState<ApiToken | null>(null);
  const [revokeError, setRevokeError] = useState<string | null>(null);
  const items = tokens.data?.items ?? [];
  const activeCount = items.filter((token) => tokenStatus(token) === 'active').length;
  const atLimit = !!scopes.data && activeCount >= scopes.data.max_tokens;
  const error = tokens.isError ? tokens.error : scopes.isError ? scopes.error : null;
  const handleRevoke = async () => {
    if (!target) return;
    setRevokeError(null);
    try {
      await revoke.mutateAsync(target.id);
      setTarget(null);
      revoke.reset();
    } catch (error) {
      setRevokeError(error instanceof ApiError ? error.message : t('common.error_generic'));
    }
  };
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <a
          className="inline-flex items-center gap-1.5 text-body text-brand-ink underline-offset-4 hover:underline"
          href={`https://tgproxypanel.com/${i18n.language.startsWith('ru') ? '' : 'en/'}api/`}
          target="_blank"
          rel="noopener noreferrer"
        >
          {t('api_tokens.documentation')}
          <ExternalLink className="size-3.5" aria-hidden="true" />
        </a>
        <Button type="button" onClick={() => setCreateOpen(true)} disabled={loading || !scopes.data || !tokens.data || atLimit}>
          <Plus />
          {t('api_tokens.create')}
        </Button>
      </div>
      {scopes.data && (
        <p className="text-label text-mute">{t('api_tokens.count', { count: activeCount, max: scopes.data.max_tokens })}</p>
      )}
      {atLimit && (
        <p role="status" className="text-body text-warn">
          {t('api_tokens.limit', { max: scopes.data?.max_tokens })}
        </p>
      )}
      {error ? (
        <ErrorState
          message={error instanceof ApiError ? error.message : t('common.error_generic')}
          retryLabel={t('common.refresh')}
          onRetry={() => {
            void tokens.refetch();
            void scopes.refetch();
          }}
        />
      ) : tokens.isLoading || scopes.isLoading ? (
        <Skeleton className="h-48 w-full rounded-surface" />
      ) : items.length ? (
        <Panel>
          <div
            className="hidden grid-cols-[minmax(0,1.15fr)_minmax(0,1.5fr)_minmax(0,1fr)_auto] gap-4 border-b border-hairline px-4 py-3 text-label text-mute md:grid"
            aria-hidden="true"
          >
            <span>{t('api_tokens.token')}</span>
            <span>{t('api_tokens.permissions')}</span>
            <span>{t('api_tokens.activity')}</span>
            <span className="w-20" />
          </div>
          <ul className="divide-y divide-hairline" aria-label={t('api_tokens.list')}>
            {items.map((token) => (
              <TokenRow
                key={token.id}
                token={token}
                onRevoke={() => {
                  setRevokeError(null);
                  setTarget(token);
                }}
              />
            ))}
          </ul>
        </Panel>
      ) : (
        <EmptyState icon={KeyRound} title={t('api_tokens.empty_title')} description={t('api_tokens.empty_description')} />
      )}
      {createOpen && scopes.data && (
        <CreateTokenDialog catalog={scopes.data} isWriter={isWriter} onClose={() => setCreateOpen(false)} />
      )}
      <Dialog
        open={!!target}
        onOpenChange={(open) => {
          if (!open && !revoke.isPending) {
            setTarget(null);
            setRevokeError(null);
            revoke.reset();
          }
        }}
      >
        <DialogContent showCloseButton={!revoke.isPending}>
          <DialogHeader>
            <DialogTitle>{t('api_tokens.revoke_title', { name: target?.name ?? '' })}</DialogTitle>
            <DialogDescription>{t('api_tokens.revoke_description')}</DialogDescription>
          </DialogHeader>
          {revokeError && (
            <p role="alert" className="text-body text-destructive">
              {revokeError}
            </p>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={revoke.isPending}
              onClick={() => {
                setTarget(null);
                setRevokeError(null);
                revoke.reset();
              }}
            >
              {t('common.cancel')}
            </Button>
            <Button type="button" variant="destructive-solid" disabled={revoke.isPending} onClick={() => void handleRevoke()}>
              {t('api_tokens.revoke')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
