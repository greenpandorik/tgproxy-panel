import { zodResolver } from '@hookform/resolvers/zod';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { useChangePassword } from '@/api/auth';
import { useAuth } from '@/auth/AuthProvider';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';

import { TotpSection } from './TotpSection';

const schema = z
  .object({
    current: z.string().min(1),
    next: z.string().min(10),
    confirm: z.string().min(1),
  })
  .refine((v) => v.next === v.confirm, { path: ['confirm'], message: 'mismatch' });

type FormValues = z.infer<typeof schema>;

export function SecurityForm() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const changePassword = useChangePassword();

  const {
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { current: '', next: '', confirm: '' },
  });

  const onSubmit = async (values: FormValues) => {
    try {
      await changePassword.mutateAsync({ current: values.current, new: values.next });
      reset();
      toast.add({ description: t('settings.security_success'), type: 'success' });
    } catch (err) {
      if (err instanceof ApiError && err.fields.current) {
        setError('current', { message: t('settings.security_error_current') });
        return;
      }
      if (err instanceof ApiError && err.fields.new) {
        setError('next', { message: t('settings.security_error_min') });
        return;
      }
      setError('root', { message: err instanceof ApiError ? err.message : t('common.error_generic') });
    }
  };

  return (
    <div className="flex max-w-md flex-col gap-4">
      <Panel>
        <PanelHeader title={t('settings.security_password_title')} actions={<HelpButton topic="settings.security" />} />
        <PanelBody>
          <form className="max-w-sm space-y-4" onSubmit={(e) => void handleSubmit(onSubmit)(e)} noValidate>
            <p className="text-sm text-mute">{t('settings.security_note')}</p>

            <div className="space-y-1.5">
              <Label htmlFor="security-current">{t('settings.security_current_password')}</Label>
              <Input
                id="security-current"
                type="password"
                autoComplete="current-password"
                {...register('current')}
                aria-invalid={!!errors.current}
              />
              {errors.current && <p className="text-xs text-destructive">{t('common.required')}</p>}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="security-new">{t('settings.security_new_password')}</Label>
              <Input id="security-new" type="password" autoComplete="new-password" {...register('next')} aria-invalid={!!errors.next} />
              {errors.next && <p className="text-xs text-destructive">{t('settings.security_error_min')}</p>}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="security-confirm">{t('settings.security_confirm_password')}</Label>
              <Input
                id="security-confirm"
                type="password"
                autoComplete="new-password"
                {...register('confirm')}
                aria-invalid={!!errors.confirm}
              />
              {errors.confirm && <p className="text-xs text-destructive">{t('settings.security_error_mismatch')}</p>}
            </div>

            {errors.root && (
              <p role="alert" className="flex items-start gap-2 text-sm text-destructive">
                <span className="mt-[6px] size-[7px] shrink-0 rounded-full bg-err" aria-hidden="true" />
                <span className="min-w-0 flex-1">{errors.root.message}</span>
              </p>
            )}

            <Button type="submit" disabled={isSubmitting}>
              {t('settings.security_submit')}
            </Button>
          </form>
        </PanelBody>
      </Panel>

      {/* Second factor only when the panel was started with FEATURE_TOTP. */}
      {user?.features.totp && <TotpSection />}
    </div>
  );
}
