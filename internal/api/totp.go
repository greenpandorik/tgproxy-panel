package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/qrlink"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/totp"
)

const (
	// recoveryCodeCount is how many one-time codes an enrolment mints. Eight is the
	// common denominator across the panels users are used to; more codes only
	// increase the amount of secret material lying around in a text file.
	recoveryCodeCount = 8
	// challengeTTL bounds the gap between "password accepted" and "second factor
	// accepted". Short enough that a challenge leaked from a browser history or a
	// proxy log is worthless, long enough to find a phone and read a code.
	challengeTTL = 5 * time.Minute
	// totpQRSize is the PNG edge in pixels. 256 keeps the data URI small while
	// staying comfortably scannable on a laptop screen.
	totpQRSize = 256
)

// featureEnabled short-circuits the whole TOTP surface when FEATURE_TOTP is off.
// A 404 (rather than a 403) is deliberate: with the flag off the endpoint does not
// exist as far as the product is concerned, and an unauthenticated caller learns
// nothing about the install from probing it.
func (s *Server) totpEnabled(w http.ResponseWriter) bool {
	if s.cfg.FeatureTOTP {
		return true
	}
	writeError(w, 404, "feature_disabled", "two-factor authentication is disabled", nil)
	return false
}

// totpIssuer is the label an authenticator app shows next to the code. It follows
// the panel name so a user with several installs can tell them apart; the branding
// read is best-effort because a missing profile must not block enrolment.
func (s *Server) totpIssuer(ctx context.Context) string {
	if b, err := s.store.Q.GetActiveBranding(ctx); err == nil && strings.TrimSpace(b.PanelName) != "" {
		return strings.TrimSpace(b.PanelName)
	}
	if u, err := url.Parse(s.cfg.PublicURL); err == nil && u.Host != "" {
		return u.Host
	}
	return "TGWebProxy"
}

type totpSetupResp struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauth_url"`
	QRDataURI  string `json:"qr_data_uri"`
}

// handleTOTPSetup mints a candidate secret and parks it in totp_pending_enc. It is
// deliberately repeatable: a user who loses the QR code before confirming just runs
// setup again, and because the pending secret is separate from the live one, doing
// so can never weaken an enrolment that is already protecting the account.
func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	if !s.totpEnabled(w) {
		return
	}
	p, _ := PrincipalFrom(r.Context())
	secret, err := totp.GenerateSecret()
	if err != nil {
		s.log.Error("totp: generate secret", "err", err)
		internal(w)
		return
	}
	enc, err := s.box.EncryptString(secret)
	if err != nil {
		s.log.Error("totp: encrypt secret", "err", err)
		internal(w)
		return
	}
	if err := s.store.Q.SetAdminTOTPPending(r.Context(), db.SetAdminTOTPPendingParams{ID: p.UserID, TotpPendingEnc: enc}); err != nil {
		s.log.Error("totp: store pending secret", "err", err)
		internal(w)
		return
	}
	otpauth := totp.ProvisioningURL(s.totpIssuer(r.Context()), p.Username, secret)
	qr, err := qrlink.DataURI(otpauth, totpQRSize)
	if err != nil {
		s.log.Error("totp: render qr", "err", err)
		internal(w)
		return
	}
	// No audit entry here: starting an enrolment changes nothing that survives, and
	// the entry that matters (auth.totp_enabled) is written on confirm.
	writeJSON(w, 200, totpSetupResp{Secret: secret, OtpauthURL: otpauth, QRDataURI: qr})
}

type totpConfirmReq struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// handleTOTPConfirm promotes the pending secret once the user proves their app has
// it, and mints the recovery codes in the same transaction: an enrolment that ended
// up enabled without codes would be one lost phone away from a permanent lockout.
//
// It demands the current password, exactly as disable does. Without that, an
// attacker holding a hijacked session could enrol *their own* authenticator and
// turn temporary access into a lockout the real owner cannot undo: they know the
// password but cannot produce a code, and every route that could turn the factor
// off needs one. The password is the thing a session thief does not have.
func (s *Server) handleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.totpEnabled(w) {
		return
	}
	p, _ := PrincipalFrom(r.Context())
	var req totpConfirmReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	row, err := s.store.Q.GetAdmin(r.Context(), p.UserID)
	if err != nil {
		internal(w)
		return
	}
	if ok, _ := crypto.VerifyPassword(req.Password, row.PasswordHash); !ok {
		validation(w, map[string]string{"password": "wrong password"})
		return
	}
	if len(row.TotpPendingEnc) == 0 {
		conflict(w, "start a setup first")
		return
	}
	secret, err := s.box.DecryptString(row.TotpPendingEnc)
	if err != nil {
		s.log.Error("totp: decrypt pending secret", "err", err)
		internal(w)
		return
	}
	if !totp.Validate(secret, strings.TrimSpace(req.Code), time.Now()) {
		validation(w, map[string]string{"code": "wrong code"})
		return
	}
	enc, err := s.box.EncryptString(secret)
	if err != nil {
		s.log.Error("totp: encrypt secret", "err", err)
		internal(w)
		return
	}
	codes, err := totp.NewRecoveryCodes(recoveryCodeCount)
	if err != nil {
		s.log.Error("totp: generate recovery codes", "err", err)
		internal(w)
		return
	}
	err = s.store.Tx(r.Context(), func(q *db.Queries) error {
		if err := q.SetAdminTOTP(r.Context(), db.SetAdminTOTPParams{ID: p.UserID, TotpSecretEnc: enc, TotpEnabled: true}); err != nil {
			return err
		}
		// Re-enrolling replaces the previous batch: codes minted for an older secret
		// must not stay valid for the new one.
		if err := q.DeleteRecoveryCodes(r.Context(), p.UserID); err != nil {
			return err
		}
		for _, c := range codes {
			if err := q.InsertRecoveryCode(r.Context(), db.InsertRecoveryCodeParams{AdminUserID: p.UserID, CodeHash: crypto.HashToken(c)}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.log.Error("totp: enable", "err", err)
		internal(w)
		return
	}
	// Every other session for this account goes, the way a password change ends
	// them: enrolling a second factor is a statement about who may hold a session
	// here, and a session opened before it must not outlive it. The caller's own
	// session is replaced in the same breath so the user is not signed out of the
	// tab they just enrolled from.
	_ = s.store.Q.DeleteUserSessions(r.Context(), p.UserID)
	if err := s.issueSession(w, r, p.UserID); err != nil {
		s.log.Error("totp: reissue session", "err", err)
		internal(w)
		return
	}
	s.Audit(r.Context(), "auth.totp_enabled", "admin", p.UserID.String(), nil)
	// The plaintext codes exist only in this response body; nothing logs them.
	writeJSON(w, 200, map[string]any{"recovery_codes": codes})
}

type totpDisableReq struct {
	Password     string `json:"password"`
	Code         string `json:"code"`
	RecoveryCode string `json:"recovery_code"`
}

// handleTOTPDisable turns the second factor off. It demands the password *and* a
// second factor for the same reason enabling demands a code: an attacker sitting on
// a live session must not be able to strip 2FA, and someone who knows the password
// but has no session cannot reach this route at all.
func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	if !s.totpEnabled(w) {
		return
	}
	p, _ := PrincipalFrom(r.Context())
	var req totpDisableReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	u, err := s.store.Q.GetAdmin(r.Context(), p.UserID)
	if err != nil {
		internal(w)
		return
	}
	if !u.TotpEnabled {
		conflict(w, "two-factor authentication is not enabled")
		return
	}
	if ok, _ := crypto.VerifyPassword(req.Password, u.PasswordHash); !ok {
		validation(w, map[string]string{"password": "wrong password"})
		return
	}
	if !s.acceptSecondFactor(r.Context(), u, strings.TrimSpace(req.Code), strings.TrimSpace(req.RecoveryCode)) {
		validation(w, map[string]string{"code": "wrong code"})
		return
	}
	err = s.store.Tx(r.Context(), func(q *db.Queries) error {
		if err := q.SetAdminTOTP(r.Context(), db.SetAdminTOTPParams{ID: p.UserID, TotpSecretEnc: nil, TotpEnabled: false}); err != nil {
			return err
		}
		return q.DeleteRecoveryCodes(r.Context(), p.UserID)
	})
	if err != nil {
		s.log.Error("totp: disable", "err", err)
		internal(w)
		return
	}
	s.Audit(r.Context(), "auth.totp_disabled", "admin", p.UserID.String(), nil)
	w.WriteHeader(204)
}

// acceptSecondFactor checks a TOTP code or burns a recovery code. Exactly one of the
// two is expected; a recovery code is consumed even when it is presented to the
// disable endpoint, because a code that has been typed anywhere is spent.
func (s *Server) acceptSecondFactor(ctx context.Context, u db.AdminUser, code, recovery string) bool {
	if recovery != "" {
		n, err := s.store.Q.UseRecoveryCode(ctx, db.UseRecoveryCodeParams{
			AdminUserID: u.ID, CodeHash: crypto.HashToken(strings.ToLower(recovery)),
		})
		return err == nil && n == 1
	}
	if code == "" || len(u.TotpSecretEnc) == 0 {
		return false
	}
	secret, err := s.box.DecryptString(u.TotpSecretEnc)
	if err != nil {
		s.log.Error("totp: decrypt secret", "err", err)
		return false
	}
	return totp.Validate(secret, code, time.Now())
}

// --- login challenge ------------------------------------------------------

// challengePrefix is domain separation, nothing more. The same crypto.Signer signs
// session cookies, so without a marker of its own a challenge is only "not a valid
// session id" by accident of two formats never colliding - a property nobody will
// re-check the day a third use of the signer appears. With the prefix, a token
// minted for one purpose can never be verified as the other.
const challengePrefix = "totp:"

// challengeState says why a challenge was refused. The distinction is only ever
// surfaced for expiry: a stale deadline is not a secret (it is embedded in a token
// the caller already holds and is unforgeable), while everything else must stay
// indistinguishable from a wrong code.
type challengeState int

const (
	challengeInvalid challengeState = iota
	challengeExpired
	challengeValid
)

// newTOTPChallenge signs "totp:<user id>|<unix expiry>". Nothing is stored: the HMAC
// is what makes the token unforgeable and the embedded expiry is what makes it
// short-lived, so a second factor never costs a database row or a cleanup job.
func (s *Server) newTOTPChallenge(userID uuid.UUID, now time.Time) string {
	return s.signer.Sign(challengePrefix + userID.String() + "|" + strconv.FormatInt(now.Add(challengeTTL).Unix(), 10))
}

// parseTOTPChallenge returns the user the challenge was issued for, together with
// why it was refused when it was: a wrong signature, a payload that is not a
// challenge, or a deadline that has passed.
func (s *Server) parseTOTPChallenge(token string, now time.Time) (uuid.UUID, challengeState) {
	value, ok := s.signer.Verify(token)
	if !ok {
		return uuid.Nil, challengeInvalid
	}
	payload, ok := strings.CutPrefix(value, challengePrefix)
	if !ok {
		return uuid.Nil, challengeInvalid
	}
	idStr, expStr, ok := strings.Cut(payload, "|")
	if !ok {
		return uuid.Nil, challengeInvalid
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return uuid.Nil, challengeInvalid
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, challengeInvalid
	}
	// The id is parsed before the deadline is judged so that a well-formed but
	// stale challenge is reported as expired and a malformed one never is.
	if now.After(time.Unix(exp, 0)) {
		return uuid.Nil, challengeExpired
	}
	return id, challengeValid
}

type totpVerifyReq struct {
	Challenge    string `json:"challenge"`
	Code         string `json:"code"`
	RecoveryCode string `json:"recovery_code"`
}

// handleTOTPVerify is the second half of login. It is a public route: the caller has
// no session yet, and the challenge issued by POST /auth/login is what authenticates
// the request. Every rejection is the same 401 so the endpoint cannot be used to tell
// a valid challenge from an invalid one.
func (s *Server) handleTOTPVerify(w http.ResponseWriter, r *http.Request) {
	if !s.totpEnabled(w) {
		return
	}
	// The same limiter as the password step: without it the second factor would be
	// the one login surface an attacker could hammer at full speed. Only wrong codes
	// are charged to it (below), so a normal two-step login costs one slot, not two.
	ip := ipFrom(r.Context())
	if s.loginLimiter.Blocked(ip) {
		writeError(w, 429, "rate_limited", "too many attempts, try later", nil)
		return
	}
	var req totpVerifyReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	invalid := func() { writeError(w, 401, "invalid_code", "wrong code", nil) }

	userID, state := s.parseTOTPChallenge(req.Challenge, time.Now())
	if state == challengeExpired {
		// Told apart from a wrong code on purpose: the user who stepped away for
		// six minutes needs "start again", not "your authenticator is broken", and
		// a challenge's own expiry gives an attacker nothing they did not already
		// hold. It is not charged to the limiter either - waiting is not guessing.
		writeError(w, 401, "challenge_expired", "this sign-in attempt expired, start again", nil)
		return
	}
	if state != challengeValid {
		invalid()
		return
	}
	u, err := s.store.Q.GetAdmin(r.Context(), userID)
	if err != nil || !u.TotpEnabled {
		invalid()
		return
	}
	if u.LockedUntil != nil && u.LockedUntil.After(time.Now()) {
		writeError(w, 423, "locked", "account temporarily locked", nil)
		return
	}
	usedRecovery := strings.TrimSpace(req.RecoveryCode) != ""
	if !s.acceptSecondFactor(r.Context(), u, strings.TrimSpace(req.Code), strings.TrimSpace(req.RecoveryCode)) {
		// A wrong second factor is a failed login attempt like any other: it feeds both
		// the per-account counter that eventually locks the account and the per-IP
		// limiter that stops one address from spraying codes across many accounts.
		_ = s.store.Q.RecordFailedLogin(r.Context(), u.ID)
		_ = s.loginLimiter.Allow(ip)
		invalid()
		return
	}
	_ = s.store.Q.ResetFailedLogins(r.Context(), u.ID)
	if err := s.issueSession(w, r, u.ID); err != nil {
		s.log.Error("issue session", "err", err)
		internal(w)
		return
	}
	meta := map[string]any{"totp": true}
	if usedRecovery {
		meta = map[string]any{"recovery": true}
	}
	ctx := withPrincipal(r.Context(), Principal{UserID: u.ID, Username: u.Username, Role: string(u.Role)})
	s.Audit(ctx, "auth.login", "admin", u.ID.String(), meta)
	writeJSON(w, 200, s.meJSON(u.ID, u.Username, string(u.Role), u.TotpEnabled))
}
