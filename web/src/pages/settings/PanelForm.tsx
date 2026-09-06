import { zodResolver } from '@hookform/resolvers/zod';
import { useEffect } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { usePutSettings, useSettings, useTelegramTest } from '@/api/settings';
import { useAuth } from '@/auth/AuthProvider';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { Switch } from '@/components/ui/switch';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';
import { cn } from '@/lib/utils';

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
    defaultValues: {
      apply_interval: 30,
      offline_after: 90,
      telegram_enabled: false,
      chat_id: '',
      bot_token: '',
      clear_token: false,
    },
  });

  useEffect(() => {
    if (settingsQuery.data) reset(valuesFromSettings(settingsQuery.data));
  }, [settingsQuery.data, reset]);

  const telegramEnabled = watch('telegram_enabled');
  const clearToken = watch('clear_token');
  const botTokenValue = watch('bot_token');
  const chatIdValue = watch('chat_id');
  const tokenSet = settingsQuery.data?.telegram_alerts.bot_token_set ?? false;

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
    return (
      <div className="max-w-xl space-y-3">
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  const disabled = !isOwner;

  return (
    <form className="flex max-w-xl flex-col gap-4" onSubmit={(e) => void handleSubmit(onSubmit)(e)} noValidate>
      {!isOwner && <p className="text-sm text-mute">{t('settings.panel_owner_only_note')}</p>}

      <Panel>
        <PanelHeader title={t('settings.panel_section_intervals')} />
        <PanelBody className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="panel-apply-interval">{t('settings.panel_apply_interval')}</Label>
            <Input
              id="panel-apply-interval"
              type="number"
              min={10}
              max={3600}
              className="mono max-w-32"
              disabled={disabled}
              {...register('apply_interval')}
              aria-invalid={!!errors.apply_interval}
            />
            <p className={errors.apply_interval ? 'text-xs text-destructive' : 'text-xs text-mute'}>
              {t('settings.panel_apply_interval_hint')}
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="panel-offline-after">{t('settings.panel_offline_after')}</Label>
            <Input
              id="panel-offline-after"
              type="number"
              min={30}
              max={3600}
              className="mono max-w-32"
              disabled={disabled}
              {...register('offline_after')}
              aria-invalid={!!errors.offline_after}
            />
            <p className={errors.offline_after ? 'text-xs text-destructive' : 'text-xs text-mute'}>
              {t('settings.panel_offline_after_hint')}
            </p>
          </div>
        </PanelBody>
      </Panel>

      <Panel>
        <PanelHeader title={t('settings.panel_telegram_alerts')} />
        <PanelBody className="space-y-4">
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor="panel-telegram-enabled">{t('settings.panel_telegram_enabled')}</Label>
            <Controller
              control={control}
              name="telegram_enabled"
              render={({ field }) => (
                <Switch id="panel-telegram-enabled" checked={field.value} onCheckedChange={field.onChange} disabled={disabled} />
              )}
            />
          </div>

          {telegramEnabled && (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="panel-chat-id">{t('settings.panel_telegram_chat_id')}</Label>
                <Input id="panel-chat-id" className="mono max-w-48" disabled={disabled} {...register('chat_id')} />
              </div>

              <div className="space-y-1.5">
                <div className="flex items-center justify-between gap-2">
                  <Label htmlFor="panel-bot-token">{t('settings.panel_telegram_bot_token')}</Label>
                  <span className="mono flex items-center gap-1.5 text-xs text-dim">
                    <span className={cn('size-[7px] shrink-0 rounded-full', tokenSet ? 'bg-ok' : 'bg-pending')} aria-hidden="true" />
                    {tokenSet ? t('settings.panel_telegram_token_set') : t('settings.panel_telegram_token_not_set')}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <Input
                    id="panel-bot-token"
                    type="password"
                    autoComplete="off"
                    className="mono"
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
                    {telegramTest.isPending ? t('settings.panel_telegram_test_sending') : t('settings.panel_telegram_test_button')}
                  </Button>
                </div>
                {tokenSet && (
                  <label className="flex items-center gap-2 pt-1 text-xs text-mute">
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

      {isOwner && (
        <div>
          <Button type="submit" disabled={isSubmitting || !isDirty}>
            {t('common.save')}
          </Button>
        </div>
      )}
    </form>
  );
}
