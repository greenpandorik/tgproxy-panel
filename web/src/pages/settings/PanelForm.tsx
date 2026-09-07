import { zodResolver } from '@hookform/resolvers/zod';
import { useEffect } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { usePutSettings, useSettings, useTelegramTest } from '@/api/settings';
import { useAuth } from '@/auth/AuthProvider';
import { DraftBanner } from '@/components/common/DraftBanner';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { Switch } from '@/components/ui/switch';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';
import { cn } from '@/lib/utils';

import { Arriving, FormFooter } from './formShell';

import type { Settings } from '@/api/types';

const schema = z.object({
  apply_interval: z.coerce.number().int().min(10).max(3600),
  offline_after: z.coerce.number().int().min(30).max(3600),
  telegram_enabled: z.boolean(),
  chat_id: z.string(),
  bot_token: z.string(),
  clear_token: z.boolean(),
});

type FormValues = z.infer<typeof schema>;

const DEFAULT_VALUES: FormValues = {
  apply_interval: 30,
  offline_after: 90,
  telegram_enabled: false,
  chat_id: '',
  bot_token: '',
  clear_token: false,
};

/** What the form remembers between visits - never the bot token. */
type PanelDraft = Omit<FormValues, 'bot_token'>;

function draftOf(v: FormValues): PanelDraft {
  return {
    // The number inputs hold strings once typed into; compare them as numbers
    // so "30" typed over 30 does not count as a change worth remembering.
    apply_interval: Number(v.apply_interval),
    offline_after: Number(v.offline_after),
    telegram_enabled: v.telegram_enabled,
    chat_id: v.chat_id,
    clear_token: v.clear_token,
  };
}

function valuesFromSettings(s: Settings): FormValues {
  return {
    apply_interval: s.apply_interval,
    offline_after: s.offline_after,
    telegram_enabled: s.telegram_alerts.enabled,
    chat_id: s.telegram_alerts.chat_id,
    bot_token: '',
    clear_token: false,
  };
}

export function PanelForm() {
  const { t } = useTranslation();
  const { isOwner } = useAuth();
  const settingsQuery = useSettings();
  const putSettings = usePutSettings();
  const telegramTest = useTelegramTest();

  const {
    control,
    register,
    handleSubmit,
    reset,
    watch,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: DEFAULT_VALUES,
  });

  useEffect(() => {
    if (settingsQuery.data) reset(valuesFromSettings(settingsQuery.data));
  }, [settingsQuery.data, reset]);

  const values = watch();
  const telegramEnabled = values.telegram_enabled;
  const clearToken = values.clear_token;
  const botTokenValue = values.bot_token;
  const chatIdValue = values.chat_id;
  const tokenSet = settingsQuery.data?.telegram_alerts.bot_token_set ?? false;

  // Only an owner can save, so only an owner gets a draft; it waits for the
  // settings to arrive so the placeholder defaults are never stored.
  const draft = useDraft<PanelDraft>('settings-panel', draftOf(values), {
    initial: draftOf(settingsQuery.data ? valuesFromSettings(settingsQuery.data) : DEFAULT_VALUES),
    open: isOwner && !!settingsQuery.data,
  });

  const resumeDraft = () => {
    if (!draft.draft) return;
    // The stored settings stay the baseline, so the form is dirty and Save enables.
    reset({ ...draft.draft.value, bot_token: values.bot_token }, { keepDefaultValues: true });
    draft.dismiss();
  };

  const onSubmit = async (values: FormValues) => {
    try {
      await putSettings.mutateAsync({
        apply_interval: values.apply_interval,
        offline_after: values.offline_after,
        telegram_alerts: {
          enabled: values.telegram_enabled,
          chat_id: values.chat_id.trim(),
          bot_token: values.clear_token ? '' : values.bot_token.trim() ? values.bot_token.trim() : undefined,
        },
      });
      draft.clear();
      toast.add({ description: t('settings.panel_save_success'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const onTestMessage = async () => {
    try {
      // Both values come from the form, not from what is stored: canTestTelegram enables the
      // button off the in-progress chat id, so sending the stored one would reject a form
      // that looks complete. The server uses them for this send only.
      const result = await telegramTest.mutateAsync({
        bot_token: botTokenValue.trim() || undefined,
        chat_id: chatIdValue.trim() || undefined,
      });
      toast.add({
        description: result.ok ? t('settings.panel_telegram_test_success') : t('common.error_generic'),
        type: result.ok ? 'success' : 'error',
      });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  const canTestTelegram = !clearToken && (tokenSet || botTokenValue.trim().length > 0) && chatIdValue.trim().length > 0;

  if (settingsQuery.isLoading) {
    // Two panels and the footer strip, in silhouette, at the sizes they land at.
    return (
      <div className="flex max-w-2xl flex-col gap-4">
        <Skeleton className="h-40 w-full rounded-surface" />
        <Skeleton className="h-48 w-full rounded-surface" />
        <Skeleton className="h-14 w-full rounded-surface" />
      </div>
    );
  }

  if (settingsQuery.isError) {
    return (
      <div className="max-w-2xl">
        <ErrorState
          message={settingsQuery.error instanceof ApiError ? settingsQuery.error.message : t('common.error_generic')}
          retryLabel={t('common.refresh')}
          onRetry={() => void settingsQuery.refetch()}
        />
      </div>
    );
  }

  const disabled = !isOwner;

  return (
    <form className="flex max-w-2xl flex-col gap-4" onSubmit={(e) => void handleSubmit(onSubmit)(e)} noValidate>
      {draft.draft && <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />}

      <Arriving>
        <Panel>
          <PanelHeader title={t('settings.panel_section_intervals')} actions={<HelpButton topic="settings.panel" />} />
          <PanelBody className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="panel-apply-interval">{t('settings.panel_apply_interval')}</Label>
              <Input
                id="panel-apply-interval"
                type="number"
                min={10}
                max={3600}
                className="mono text-mono max-w-32"
                disabled={disabled}
                {...register('apply_interval')}
                aria-invalid={!!errors.apply_interval}
                aria-describedby="panel-apply-interval-hint"
              />
              <p
                id="panel-apply-interval-hint"
                className={cn('text-label', errors.apply_interval ? 'text-destructive' : 'text-mute')}
              >
                {t('settings.panel_apply_interval_hint')}
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="panel-offline-after">{t('settings.panel_offline_after')}</Label>
              <Input
                id="panel-offline-after"
                type="number"
                min={30}
                max={3600}
                className="mono text-mono max-w-32"
                disabled={disabled}
                {...register('offline_after')}
                aria-invalid={!!errors.offline_after}
                aria-describedby="panel-offline-after-hint"
              />
              <p
                id="panel-offline-after-hint"
                className={cn('text-label', errors.offline_after ? 'text-destructive' : 'text-mute')}
              >
                {t('settings.panel_offline_after_hint')}
              </p>
            </div>
          </PanelBody>
        </Panel>
      </Arriving>

      <Arriving index={1}>
        <Panel>
          <PanelHeader title={t('settings.panel_telegram_alerts')} actions={<HelpButton topic="settings.telegram" />} />
          <PanelBody className="space-y-4">
            <div className="flex items-center justify-between gap-3">
              <Label htmlFor="panel-telegram-enabled">{t('settings.panel_telegram_enabled')}</Label>
              <Controller
                control={control}
                name="telegram_enabled"
                render={({ field }) => (
                  <Switch
                    id="panel-telegram-enabled"
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={disabled}
                  />
                )}
              />
            </div>

            {telegramEnabled && (
              <>
                <div className="space-y-2">
                  <Label htmlFor="panel-chat-id">{t('settings.panel_telegram_chat_id')}</Label>
                  <Input id="panel-chat-id" className="mono text-mono max-w-48" disabled={disabled} {...register('chat_id')} />
                </div>

                <div className="space-y-2">
                  <div className="flex items-center justify-between gap-2">
                    <Label htmlFor="panel-bot-token">{t('settings.panel_telegram_bot_token')}</Label>
                    <span className="flex items-center gap-1.5 text-label text-mute">
                      <span
                        className={cn('size-[7px] shrink-0 rounded-pill', tokenSet ? 'bg-ok' : 'bg-pending')}
                        aria-hidden="true"
                      />
                      {tokenSet ? t('settings.panel_telegram_token_set') : t('settings.panel_telegram_token_not_set')}
                    </span>
                  </div>
                  <div className="flex items-center gap-2">
                    <Input
                      id="panel-bot-token"
                      type="password"
                      autoComplete="off"
                      className="mono text-mono"
                      placeholder={t('settings.panel_telegram_bot_token_placeholder')}
                      disabled={disabled || clearToken}
                      {...register('bot_token')}
                    />
                    <Button
                      type="button"
                      variant="outline"
                      className="shrink-0"
                      disabled={disabled || !canTestTelegram || telegramTest.isPending}
                      onClick={() => void onTestMessage()}
                    >
                      {telegramTest.isPending
                        ? t('settings.panel_telegram_test_sending')
                        : t('settings.panel_telegram_test_button')}
                    </Button>
                  </div>
                  {tokenSet && (
                    <label className="flex items-center gap-2 pt-1 text-label text-mute">
                      <Controller
                        control={control}
                        name="clear_token"
                        render={({ field }) => (
                          <Checkbox checked={field.value} onCheckedChange={(v) => field.onChange(!!v)} disabled={disabled} />
                        )}
                      />
                      {t('settings.panel_telegram_clear_token')}
                    </label>
                  )}
                </div>
              </>
            )}
          </PanelBody>
        </Panel>
      </Arriving>

      <FormFooter note={!isOwner ? t('settings.panel_owner_only_note') : undefined}>
        {isOwner && (
          <Button type="submit" disabled={isSubmitting || !isDirty}>
            {t('common.save')}
          </Button>
        )}
      </FormFooter>
    </form>
  );
}
