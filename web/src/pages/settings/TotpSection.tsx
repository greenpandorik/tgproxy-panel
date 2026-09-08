import { Download, ShieldCheck } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useTotpConfirm, useTotpDisable, useTotpSetup } from '@/api/auth';
import { useAuth } from '@/auth/AuthProvider';
import { CopyButton } from '@/components/common/CopyButton';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { ApiError } from '@/lib/api';
import { cn } from '@/lib/utils';

import { Arriving, FormFooter } from './formShell';

import type { TotpSetup } from '@/api/types';

/**
 * Base UI reports why a dialog wants to close. These are the reasons that mean "the
 * user dismissed it" rather than "the app closed it deliberately"; the recovery-codes
 * dialog refuses all of them.
 */
const DISMISSALS = new Set<string>(['outside-press', 'escape-key', 'close-press', 'focus-out']);

/** Shared shape for the two short code fields (enrolment confirm, disable). */
/*
 * The six-digit code fields. tracking-code is the one non-micro letter
 * spacing in the panel and it is not a typographic choice: the eye has to
 * count six characters here rather than read a word, so the digits are set as
 * separate cells. The token is named in index.css so both OTP inputs share
 * one number. The placeholder rides the same spacing, or the dots would sit
 * where the digits will not.
 */
const codeFieldClass = 'mono h-9 text-center text-body tracking-code placeholder:tracking-code placeholder:text-mute';

/**
 * Two-factor authentication for the signed-in admin. Rendered only when the panel
 * runs with FEATURE_TOTP; it is self-service for every role, viewers included.
 */
export function TotpSection() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const setup = useTotpSetup();
  const confirm = useTotpConfirm();
  const disable = useTotpDisable();

  const [enrolment, setEnrolment] = useState<TotpSetup | null>(null);
  const [code, setCode] = useState('');
  const [password, setPassword] = useState('');
  const [codeError, setCodeError] = useState<string | null>(null);
  const [passwordError, setPasswordError] = useState<string | null>(null);
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);
  // The acknowledgement lives here rather than in the dialog so it is reset at the
  // moment a new set of codes arrives - a fresh enrolment must never inherit the tick
  // from the previous one.
  const [recoveryAcknowledged, setRecoveryAcknowledged] = useState(false);
  const [disableOpen, setDisableOpen] = useState(false);

  const enabled = user?.totp_enabled ?? false;

  const startEnrolment = async () => {
    setCode('');
    setPassword('');
    setCodeError(null);
    setPasswordError(null);
    try {
      setEnrolment(await setup.mutateAsync());
    } catch {
      toast.add({ description: t('common.error_generic'), type: 'error' });
    }
  };

  const cancelEnrolment = () => {
    setEnrolment(null);
    setCode('');
    setPassword('');
    setCodeError(null);
    setPasswordError(null);
  };

  const confirmEnrolment = async () => {
    setCodeError(null);
    setPasswordError(null);
    try {
      const res = await confirm.mutateAsync({ password, code: code.trim() });
      setEnrolment(null);
      setCode('');
      setPassword('');
      setRecoveryAcknowledged(false);
      setRecoveryCodes(res.recovery_codes);
      toast.add({ description: t('settings.totp_enabled_toast'), type: 'success' });
    } catch (err) {
      // The server answers a wrong password and a wrong code with the same 422
      // shape, distinguished only by which field it names; showing the error on
      // the wrong input is the difference between "try again" and "give up".
      if (err instanceof ApiError && err.fields.password) {
        setPasswordError(t('settings.security_error_current'));
        return;
      }
      setCodeError(err instanceof ApiError && err.fields.code ? t('settings.totp_error_code') : t('common.error_generic'));
    }
  };

  return (
    <>
      {/* Head, body, footer - the same three parts as every other settings form,
          with turning the second factor on or off as this one's commit action. */}
      <Arriving index={1}>
        <Panel>
          <PanelHeader icon={ShieldCheck} title={t('settings.totp_title')} actions={<HelpButton topic="settings.security" />} />
          <PanelBody className="space-y-2">
            <p className="flex items-center gap-2 text-body text-foreground">
              <span className={cn('size-[7px] shrink-0 rounded-pill', enabled ? 'bg-ok' : 'bg-pending')} aria-hidden="true" />
              {t(enabled ? 'settings.totp_status_on' : 'settings.totp_status_off')}
            </p>
            <p className="max-w-prose text-label text-mute">
              {t(enabled ? 'settings.totp_on_description' : 'settings.totp_off_description')}
            </p>
          </PanelBody>
        </Panel>
      </Arriving>

      <FormFooter>
        {enabled ? (
          <Button type="button" variant="outline" onClick={() => setDisableOpen(true)}>
            {t('settings.totp_disable')}
          </Button>
        ) : (
          <Button type="button" onClick={() => void startEnrolment()} disabled={setup.isPending}>
            {t('settings.totp_enable')}
          </Button>
        )}
      </FormFooter>

      <EnrolDialog
        enrolment={enrolment}
        code={code}
        password={password}
        codeError={codeError}
        passwordError={passwordError}
        pending={confirm.isPending}
        onCodeChange={setCode}
        onPasswordChange={setPassword}
        onCancel={cancelEnrolment}
        onConfirm={() => void confirmEnrolment()}
      />

      <RecoveryCodesDialog
        codes={recoveryCodes}
        username={user?.username ?? 'admin'}
        acknowledged={recoveryAcknowledged}
        onAcknowledgedChange={setRecoveryAcknowledged}
        onClose={() => setRecoveryCodes(null)}
      />
      <DisableDialog
        open={disableOpen}
        onOpenChange={setDisableOpen}
        onDisable={(input) => disable.mutateAsync(input)}
        pending={disable.isPending}
      />
    </>
  );
}

interface EnrolDialogProps {
  enrolment: TotpSetup | null;
  code: string;
  password: string;
  codeError: string | null;
  passwordError: string | null;
  pending: boolean;
  onCodeChange: (value: string) => void;
  onPasswordChange: (value: string) => void;
  onCancel: () => void;
  onConfirm: () => void;
}

/**
 * Enrolment, start to finish, in one dialog: scan, or type the secret in by
 * hand, then prove both that the authenticator works and that the person at
 * the keyboard knows the account password. The QR keeps a white tile of its
 * own - scanners want the quiet zone light whatever theme the panel is in.
 */
function EnrolDialog({
  enrolment,
  code,
  password,
  codeError,
  passwordError,
  pending,
  onCodeChange,
  onPasswordChange,
  onCancel,
  onConfirm,
}: EnrolDialogProps) {
  const { t } = useTranslation();

  return (
    <Dialog open={!!enrolment} onOpenChange={(open) => !open && onCancel()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="flex items-center gap-1.5">
            <DialogTitle>{t('settings.totp_title')}</DialogTitle>
            <HelpButton topic="settings.security" className="-my-1.5" />
          </div>
          <DialogDescription>{t('settings.totp_scan_hint')}</DialogDescription>
        </DialogHeader>

        {enrolment && (
          <div className="space-y-4">
            <div className="flex justify-center">
              <img
                src={enrolment.qr_data_uri}
                alt={t('settings.totp_qr_alt')}
                width={168}
                height={168}
                className="size-42 rounded-surface border border-hairline-strong bg-white p-2"
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="totp-secret">{t('settings.totp_secret')}</Label>
              <div className="flex items-center gap-1">
                <Input
                  id="totp-secret"
                  readOnly
                  value={enrolment.secret}
                  className="mono text-mono"
                  onFocus={(e) => e.target.select()}
                />
                <CopyButton value={enrolment.secret} label={t('settings.totp_copy_secret')} />
              </div>
              <p className="text-label text-mute">{t('settings.totp_secret_hint')}</p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="totp-confirm-code">{t('settings.totp_code')}</Label>
              <Input
                id="totp-confirm-code"
                autoComplete="one-time-code"
                inputMode="numeric"
                maxLength={6}
                placeholder="000000"
                className={codeFieldClass}
                value={code}
                onChange={(e) => onCodeChange(e.target.value)}
                aria-invalid={!!codeError}
              />
              {codeError && <p className="text-label text-destructive">{codeError}</p>}
            </div>

            {/* The password, not the code, is what a session thief does not have:
                without it, temporary access to a signed-in tab would be enough to
                enrol a stranger's authenticator and lock the owner out. */}
            <div className="space-y-2">
              <Label htmlFor="totp-confirm-password">{t('settings.totp_confirm_password')}</Label>
              <Input
                id="totp-confirm-password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => onPasswordChange(e.target.value)}
                aria-invalid={!!passwordError}
              />
              {passwordError ? (
                <p className="text-label text-destructive">{passwordError}</p>
              ) : (
                <p className="text-label text-mute">{t('settings.totp_confirm_password_hint')}</p>
              )}
            </div>

            <p className="text-label text-mute">{t('settings.totp_confirm_note')}</p>
          </div>
        )}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancel} disabled={pending}>
            {t('common.cancel')}
          </Button>
          <Button type="button" onClick={onConfirm} disabled={pending || code.trim().length === 0 || password.length === 0}>
            {t('settings.totp_confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * The recovery codes exist in plaintext exactly once, in the confirm response. Once
 * this dialog is gone they are unrecoverable, so it cannot be dismissed by reflex:
 * Escape, an outside press and the close button are all refused, and the one button
 * that closes it stays disabled until the user has ticked the acknowledgement. The
 * two ways of getting the codes out of the browser sit next to it.
 */
interface RecoveryCodesDialogProps {
  codes: string[] | null;
  username: string;
  acknowledged: boolean;
  onAcknowledgedChange: (acknowledged: boolean) => void;
  onClose: () => void;
}

function RecoveryCodesDialog({ codes, username, acknowledged, onAcknowledgedChange, onClose }: RecoveryCodesDialogProps) {
  const { t } = useTranslation();

  const download = () => {
    if (!codes) return;
    const blob = new Blob([codes.join('\n') + '\n'], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `recovery-codes-${username}.txt`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  };

  return (
    <Dialog
      open={!!codes}
      disablePointerDismissal
      onOpenChange={(open, details) => {
        // Every dismissal route the component offers by default would throw away the
        // only copy of the codes, so all of them are refused here; the acknowledgement
        // button calls onClose directly and never comes through this handler.
        if (!open && DISMISSALS.has(details.reason)) return;
        if (!open) onClose();
      }}
    >
      <DialogContent className="sm:max-w-lg" showCloseButton={false}>
        <DialogHeader>
          <div className="flex items-center gap-1.5">
            <DialogTitle>{t('settings.totp_recovery_title')}</DialogTitle>
            <HelpButton topic="settings.security" className="-my-1.5" />
          </div>
          <DialogDescription>{t('settings.totp_recovery_description')}</DialogDescription>
        </DialogHeader>

        <p className="flex items-start gap-2 text-body text-destructive">
          <span className="mt-1.5 size-[7px] shrink-0 rounded-pill bg-err" aria-hidden="true" />
          <span className="min-w-0 flex-1">{t('settings.totp_recovery_warning')}</span>
        </p>

        <ul className="mono grid grid-cols-2 gap-x-6 gap-y-2 rounded-control border border-hairline bg-background p-4 text-body text-foreground">
          {codes?.map((c) => (
            <li key={c}>{c}</li>
          ))}
        </ul>

        <label className="flex items-start gap-2 text-body text-foreground">
          {/* No aria-label here: the wrapping <label> already names the control, and
              adding one would make a screen reader announce the sentence twice. */}
          <Checkbox className="mt-0.5" checked={acknowledged} onCheckedChange={onAcknowledgedChange} />
          <span>{t('settings.totp_recovery_acknowledge')}</span>
        </label>

        {/* Three labelled actions: the row wraps rather than pushing itself past
            the dialog's edge on a narrow viewport. */}
        <DialogFooter className="sm:flex-wrap">
          <CopyButton value={codes?.join('\n') ?? ''} label={t('common.copy')} showLabel />
          <Button type="button" variant="outline" onClick={download}>
            <Download />
            {t('settings.totp_recovery_download')}
          </Button>
          <Button type="button" onClick={onClose} disabled={!acknowledged}>
            {t('settings.totp_recovery_saved')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface DisableDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDisable: (input: { password: string; code?: string; recovery_code?: string }) => Promise<void>;
  pending: boolean;
}

/**
 * Disabling asks for the password and a second factor together: a stolen session
 * alone must not be enough to strip 2FA off the account it is sitting in.
 */
function DisableDialog({ open, onOpenChange, onDisable, pending }: DisableDialogProps) {
  const { t } = useTranslation();
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [useRecovery, setUseRecovery] = useState(false);
  const [errors, setErrors] = useState<{ password?: string; code?: string }>({});

  const close = (next: boolean) => {
    if (!next) {
      setPassword('');
      setCode('');
      setUseRecovery(false);
      setErrors({});
    }
    onOpenChange(next);
  };

  const submit = async () => {
    setErrors({});
    const value = code.trim();
    try {
      await onDisable({ password, ...(useRecovery ? { recovery_code: value.toLowerCase() } : { code: value }) });
      close(false);
      toast.add({ description: t('settings.totp_disabled_toast'), type: 'success' });
    } catch (err) {
      if (err instanceof ApiError && err.fields.password) {
        setErrors({ password: t('settings.security_error_current') });
        return;
      }
      if (err instanceof ApiError && err.fields.code) {
        setErrors({ code: t('settings.totp_error_code') });
        return;
      }
      setErrors({ code: t('common.error_generic') });
    }
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="flex items-center gap-1.5">
            <DialogTitle>{t('settings.totp_disable_title')}</DialogTitle>
            <HelpButton topic="settings.security" className="-my-1.5" />
          </div>
          <DialogDescription>{t('settings.totp_disable_description')}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="totp-disable-password">{t('settings.security_current_password')}</Label>
            <Input
              id="totp-disable-password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              aria-invalid={!!errors.password}
            />
            {errors.password && <p className="text-label text-destructive">{errors.password}</p>}
          </div>
          <div className="space-y-2">
            <Label htmlFor="totp-disable-code">{t(useRecovery ? 'settings.totp_recovery_code' : 'settings.totp_code')}</Label>
            <Input
              id="totp-disable-code"
              autoComplete={useRecovery ? 'off' : 'one-time-code'}
              inputMode={useRecovery ? 'text' : 'numeric'}
              maxLength={useRecovery ? 11 : 6}
              spellCheck={false}
              placeholder={useRecovery ? 'xxxxx-xxxxx' : '000000'}
              className={codeFieldClass}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              aria-invalid={!!errors.code}
            />
            {errors.code && <p className="text-label text-destructive">{errors.code}</p>}
            <button
              type="button"
              className="text-label text-mute underline underline-offset-2 transition-colors hover:text-foreground"
              onClick={() => {
                setUseRecovery((v) => !v);
                setCode('');
                setErrors({});
              }}
            >
              {t(useRecovery ? 'settings.totp_use_app_code' : 'settings.totp_use_recovery')}
            </button>
          </div>
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => close(false)} disabled={pending}>
            {t('common.cancel')}
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={() => void submit()}
            disabled={pending || password.length === 0 || code.trim().length === 0}
          >
            {t('settings.totp_disable')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
