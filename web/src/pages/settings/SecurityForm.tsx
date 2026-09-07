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
import { cn } from '@/lib/utils';

import { Arriving, FormFooter } from './formShell';

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
    /*
     * Every settings form is built the same way: a panel whose header says what
     * the section is, a body that groups the fields, and a footer strip holding
     * the one action that commits them. The seven forms behind these tabs used
     * to each put their button somewhere different.
     */
    <div className="flex max-w-2xl flex-col gap-4">
      <form className="flex flex-col gap-4" onSubmit={(e) => void handleSubmit(onSubmit)(e)} noValidate>
        <Arriving>
          <Panel>
            <PanelHeader title={t('settings.security_password_title')} actions={<HelpButton topic="settings.security" />} />
            <PanelBody className="max-w-sm space-y-4">
              <p className="text-label text-mute">{t('settings.security_note')}</p>

              <div className="space-y-2">
                <Label htmlFor="security-current">{t('settings.security_current_password')}</Label>
                <Input
                  id="security-current"
                  type="password"
                  autoComplete="current-password"
                  {...register('current')}
                  aria-invalid={!!errors.current}
                  aria-describedby={errors.current ? 'security-current-error' : undefined}
                />
                {errors.current && (
                  <p id="security-current-error" className="text-label text-destructive">
                    {t('common.required')}
                  </p>
                )}
              </div>

              <div className="space-y-2">
                <Label htmlFor="security-new">{t('settings.security_new_password')}</Label>
                <Input
                  id="security-new"
                  type="password"
                  autoComplete="new-password"
                  {...register('next')}
                  aria-invalid={!!errors.next}
                  aria-describedby="security-new-hint"
                />
                <p id="security-new-hint" className={cn('text-label', errors.next ? 'text-destructive' : 'text-mute')}>
                  {t('settings.security_error_min')}
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="security-confirm">{t('settings.security_confirm_password')}</Label>
                <Input
                  id="security-confirm"
                  type="password"
                  autoComplete="new-password"
                  {...register('confirm')}
                  aria-invalid={!!errors.confirm}
                  aria-describedby={errors.confirm ? 'security-confirm-error' : undefined}
                />
                {errors.confirm && (
                  <p id="security-confirm-error" className="text-label text-destructive">
                    {t('settings.security_error_mismatch')}
                  </p>
                )}
              </div>

              {errors.root && (
                <p role="alert" className="flex items-start gap-2 text-body text-destructive">
                  <span className="mt-1.5 size-[7px] shrink-0 rounded-pill bg-err" aria-hidden="true" />
                  <span className="min-w-0 flex-1">{errors.root.message}</span>
                </p>
              )}
            </PanelBody>
          </Panel>
        </Arriving>

        <FormFooter>
          <Button type="submit" disabled={isSubmitting}>
            {t('settings.security_submit')}
          </Button>
        </FormFooter>
      </form>

      {/* Second factor only when the panel was started with FEATURE_TOTP. */}
      {user?.features.totp && <TotpSection />}
    </div>
  );
}
