import { zodResolver } from '@hookform/resolvers/zod';
import type { ReactNode } from 'react';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { z } from 'zod';

import { useBranding } from '@/api/branding';
import { isTotpChallenge } from '@/api/types';
import { useAuth } from '@/auth/AuthProvider';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ENTER_CLASS } from '@/components/ui/motion';
import type { Lang } from '@/i18n';
import { setLang } from '@/i18n';
import { ApiError } from '@/lib/api';
import { useMediaQuery } from '@/lib/useMediaQuery';
import { cn } from '@/lib/utils';

import { LoginStatusPanel, LoginWordmark } from './LoginStatusPanel';

const schema = z.object({
  username: z.string().min(1),
  password: z.string().min(1),
});

type FormValues = z.infer<typeof schema>;

const codeSchema = z.object({ code: z.string().min(1) });

type CodeValues = z.infer<typeof codeSchema>;

const ERROR_KEY: Record<string, string> = {
  invalid_credentials: 'auth.error_invalid_credentials',
  invalid_code: 'auth.error_invalid_code',
  challenge_expired: 'auth.error_challenge_expired',
  rate_limited: 'auth.error_rate_limited',
  locked: 'auth.error_locked',
};

// The split layout's breakpoint.
const WIDE = '(min-width: 900px)';

/** Field label and control, at the login screen's own (larger) scale. */
function Field({ id, label, error, children }: { id: string; label: string; error?: boolean; children: ReactNode }) {
  const { t } = useTranslation();
  return (
    <div className="space-y-2">
      <Label htmlFor={id} className="text-label font-normal text-mute">
        {label}
      </Label>
      {children}
      {error && <p className="text-label text-destructive">{t('common.required')}</p>}
    </div>
  );
}

/** `↵`, the hint that Enter submits. Hidden from the accessible name of the button. */
function EnterHint() {
  return (
    <kbd
      aria-hidden="true"
      className="mono ml-1.5 rounded-control border border-background/25 px-1.5 py-px text-micro text-background/55"
    >
      ↵
    </kbd>
  );
}

/** `ru · en`, mono and dim, with the current language in full contrast. */
function LanguageSwitch() {
  const { t, i18n } = useTranslation();
  const current = (i18n.language?.startsWith('en') ? 'en' : 'ru') as Lang;

  return (
    <div className="mono flex items-center gap-1.5 text-mono" role="group" aria-label={t('common.language')}>
      {(['ru', 'en'] as const).map((lang, i) => (
        <span key={lang} className="flex items-center gap-1.5">
          {i > 0 && <span aria-hidden="true">·</span>}
          <button
            type="button"
            onClick={() => setLang(lang)}
            aria-pressed={current === lang}
            className={cn(
              'rounded-control transition-[color,scale] duration-fast ease-out active:scale-[0.985]',
              current === lang ? 'text-foreground' : 'hover:text-mute',
            )}
          >
            {lang}
          </button>
        </span>
      ))}
    </div>
  );
}

export function LoginPage() {
  const { t } = useTranslation();
  const { user, login, verifyTotp } = useAuth();
  const { data: branding } = useBranding();
  const navigate = useNavigate();
  const location = useLocation();
  const wide = useMediaQuery(WIDE);
  const [formError, setFormError] = useState<string | null>(null);
  const [challenge, setChallenge] = useState<string | null>(null);
  const [useRecovery, setUseRecovery] = useState(false);
  const [expired, setExpired] = useState(false);

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({ resolver: zodResolver(schema) });

  const codeForm = useForm<CodeValues>({ resolver: zodResolver(codeSchema), defaultValues: { code: '' } });

  if (user) {
    const from = (location.state as { from?: string } | null)?.from;
    return <Navigate to={from && from !== '/login' ? from : '/'} replace />;
  }

  const describe = (err: unknown) =>
    t(err instanceof ApiError ? (ERROR_KEY[err.code] ?? 'common.error_generic') : 'common.error_generic');

  const onSubmit = async (values: FormValues) => {
    setFormError(null);
    try {
      const result = await login(values.username, values.password);
      if (isTotpChallenge(result)) {
        setChallenge(result.challenge);
        return;
      }
      navigate('/', { replace: true });
    } catch (err) {
      setFormError(describe(err));
    }
  };

  const onSubmitCode = async (values: CodeValues) => {
    if (!challenge) return;
    setFormError(null);
    try {
      const code = values.code.trim();
      await verifyTotp(challenge, useRecovery ? { recovery_code: code.toLowerCase() } : { code });
      navigate('/', { replace: true });
    } catch (err) {
      setFormError(describe(err));
      setExpired(err instanceof ApiError && err.code === 'challenge_expired');
      codeForm.setValue('code', '');
    }
  };

  const restart = () => {
    setChallenge(null);
    setUseRecovery(false);
    setExpired(false);
    setFormError(null);
    codeForm.reset();
  };

  // A hairline row above the button, where the eye already is.
  const errorBanner = formError && (
    <p
      role="alert"
      className="rounded-control border border-destructive/35 bg-destructive/10 px-3 py-2 text-label text-destructive"
    >
      {formError}
    </p>
  );

  return (
    <div className="grid min-h-screen grid-cols-1 bg-surface min-[900px]:grid-cols-[1fr_520px]">
      {wide && <LoginStatusPanel />}

      <main className="flex flex-col justify-center bg-surface px-6 py-12 min-[900px]:px-16 min-[900px]:py-10">
        <div className={cn(ENTER_CLASS, 'mx-auto w-full max-w-[400px]')}>
          {/* On a phone the panel is gone, so the operator's mark moves here. */}
          {!wide && <LoginWordmark className="mb-8" />}
          <h1 className="text-display text-foreground">{challenge ? t('auth.totp_title') : t('auth.login_title')}</h1>
          <p className="mt-2 text-body text-mute">
            {challenge ? t(useRecovery ? 'auth.totp_subtitle_recovery' : 'auth.totp_subtitle') : t('auth.login_subtitle')}
          </p>
          {!challenge && branding?.login_text && <p className="mt-2 text-body text-mute">{branding.login_text}</p>}

          {challenge ? (
            <form className="mt-8 space-y-5" onSubmit={(e) => void codeForm.handleSubmit(onSubmitCode)(e)} noValidate>
              <Field
                id="totp-code"
                label={t(useRecovery ? 'auth.totp_recovery_code' : 'auth.totp_code')}
                error={!!codeForm.formState.errors.code}
              >
                {/* One-time-code autocomplete lets phones and password managers fill the
                    field directly; the wide tracking is there because the number is read
                    off another screen and transcribed digit by digit. */}
                <Input
                  id="totp-code"
                  autoFocus
                  autoComplete={useRecovery ? 'off' : 'one-time-code'}
                  inputMode={useRecovery ? 'text' : 'numeric'}
                  maxLength={useRecovery ? 11 : 6}
                  spellCheck={false}
                  placeholder={useRecovery ? 'xxxxx-xxxxx' : '000000'}
                  className="mono h-10 px-3 text-center text-title tracking-code placeholder:tracking-code placeholder:text-mute"
                  {...codeForm.register('code')}
                  aria-invalid={!!codeForm.formState.errors.code}
                />
              </Field>

              {errorBanner}

              {expired ? (
                <Button type="button" size="lg" className="w-full" onClick={restart}>
                  {t('auth.totp_back')}
                </Button>
              ) : (
                <Button type="submit" size="lg" className="w-full" disabled={codeForm.formState.isSubmitting}>
                  {codeForm.formState.isSubmitting ? t('auth.totp_verifying') : t('auth.totp_verify')}
                  {!codeForm.formState.isSubmitting && <EnterHint />}
                </Button>
              )}

              <div className="flex items-center justify-between text-label">
                <button
                  type="button"
                  className="text-mute underline-offset-2 hover:text-foreground hover:underline"
                  onClick={() => {
                    setUseRecovery((v) => !v);
                    setExpired(false);
                    setFormError(null);
                    codeForm.reset();
                  }}
                >
                  {t(useRecovery ? 'auth.totp_use_app_code' : 'auth.totp_use_recovery')}
                </button>
                <button
                  type="button"
                  className="text-mute underline-offset-2 hover:text-foreground hover:underline"
                  onClick={restart}
                >
                  {t('auth.totp_back')}
                </button>
              </div>
            </form>
          ) : (
            <form className="mt-8 space-y-5" onSubmit={(e) => void handleSubmit(onSubmit)(e)} noValidate>
              <Field id="username" label={t('auth.username')} error={!!errors.username}>
                <Input
                  id="username"
                  autoComplete="username"
                  autoFocus
                  className="h-10 px-3"
                  {...register('username')}
                  aria-invalid={!!errors.username}
                />
              </Field>
              <Field id="password" label={t('auth.password')} error={!!errors.password}>
                <Input
                  id="password"
                  type="password"
                  autoComplete="current-password"
                  className="h-10 px-3"
                  {...register('password')}
                  aria-invalid={!!errors.password}
                />
              </Field>

              {errorBanner}

              <Button type="submit" size="lg" className="w-full" disabled={isSubmitting}>
                {isSubmitting ? t('auth.signing_in') : t('auth.sign_in')}
                {!isSubmitting && <EnterHint />}
              </Button>
            </form>
          )}

          <div className="mt-10 flex items-center justify-between gap-4 text-label text-mute">
            <LanguageSwitch />
            {branding?.support_link && (
              <a
                href={branding.support_link}
                target="_blank"
                rel="noreferrer"
                className="truncate underline-offset-2 hover:text-foreground hover:underline"
              >
                {t('auth.support_link_label')}
              </a>
            )}
            <span className="mono shrink-0">{branding?.footer_text || `© ${new Date().getFullYear()}`}</span>
          </div>
        </div>
      </main>
    </div>
  );
}
