package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/branding"
	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/qrlink"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/totp"
)

const (
	// recoveryCodeCount is how many one-time codes an enrolment mints.
	recoveryCodeCount = 8
	// challengeTTL bounds the gap between "password accepted" and "second factor accepted".
	challengeTTL = 5 * time.Minute
	// totpQRSize is the PNG edge in pixels.
	totpQRSize = 256
)

// featureEnabled short-circuits the whole TOTP surface when FEATURE_TOTP is off.
func (s *Server) totpEnabled(w http.ResponseWriter) bool {
	if s.cfg.FeatureTOTP {
		return true
	}
	writeError(w, 404, "feature_disabled", "two-factor authentication is disabled", nil)
	return false
}

// totpIssuer is the label an authenticator app shows next to the code.
func (s *Server) totpIssuer(ctx context.Context) string {
	if b, err := s.store.Q.GetActiveBranding(ctx); err == nil && strings.TrimSpace(b.PanelName) != "" {
		return strings.TrimSpace(b.PanelName)
	}
	if u, err := url.Parse(s.cfg.PublicURL); err == nil && u.Host != "" {
		return u.Host
	}
	return branding.DefaultPanelName
}

type totpSetupResp struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauth_url"`
	QRDataURI  string `json:"qr_data_uri"`
}

// handleTOTPSetup mints a candidate secret and parks it in totp_pending_enc.
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
	writeJSON(w, 200, totpSetupResp{Secret: secret, OtpauthURL: otpauth, QRDataURI: qr})
}

type totpConfirmReq struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

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

// handleTOTPDisable turns the second factor off.
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

// acceptSecondFactor checks a TOTP code or burns a recovery code.
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

// challengePrefix is domain separation, nothing more.
const challengePrefix = "totp:"

// challengeState says why a challenge was refused.
type challengeState int

const (
	challengeInvalid challengeState = iota
	challengeExpired
	challengeValid
)

// newTOTPChallenge signs "totp:<user id>|<unix expiry>".
func (s *Server) newTOTPChallenge(userID uuid.UUID, now time.Time) string {
	return s.signer.Sign(challengePrefix + userID.String() + "|" + strconv.FormatInt(now.Add(challengeTTL).Unix(), 10))
}

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

// handleTOTPVerify is the second half of login.
func (s *Server) handleTOTPVerify(w http.ResponseWriter, r *http.Request) {
	if !s.totpEnabled(w) {
		return
	}
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
		_ = s.store.Q.RecordFailedLogin(r.Context(), u.ID)
		_ = s.loginLimiter.Allow(ip)
		reason := "bad_totp_code"
		if usedRecovery {
			reason = "bad_recovery_code"
		}
		s.recordLoginFailure(r, reason, u.Username, u.ID.String())
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
