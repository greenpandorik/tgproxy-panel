package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	otptotp "github.com/pquerna/otp/totp"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store/db"
)

type setupBody struct {
	Secret      string `json:"secret"`
	OtpauthURL  string `json:"otpauth_url"`
	QRDataURI   string `json:"qr_data_uri"`
	TotpEnabled bool   `json:"totp_enabled"`
}

type confirmBody struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

type loginBody struct {
	TotpRequired bool   `json:"totp_required"`
	Challenge    string `json:"challenge"`
	Username     string `json:"username"`
	Role         string `json:"role"`
}

func enrol(t *testing.T, c *apitest.Client, password string) (string, []string) {
	t.Helper()
	var s setupBody
	resp := c.Post("/api/v1/auth/totp/setup", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("setup: %d", resp.StatusCode)
	}
	c.JSON(resp, &s)
	if s.Secret == "" || !strings.HasPrefix(s.OtpauthURL, "otpauth://totp/") ||
		!strings.HasPrefix(s.QRDataURI, "data:image/png;base64,") {
		t.Fatalf("setup body %+v", s)
	}

	code, err := otptotp.GenerateCode(s.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var cb confirmBody
	resp = c.Post("/api/v1/auth/totp/confirm", map[string]string{"password": password, "code": code})
	if resp.StatusCode != 200 {
		t.Fatalf("confirm: %d", resp.StatusCode)
	}
	c.JSON(resp, &cb)
	if len(cb.RecoveryCodes) != 8 {
		t.Fatalf("got %d recovery codes, want 8", len(cb.RecoveryCodes))
	}
	return s.Secret, cb.RecoveryCodes
}

func TestTOTPEnrolThenLoginWithCode(t *testing.T) {
	h := apitest.New(t, apitest.WithTOTP())
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	secret, _ := enrol(t, c, "pass-123456")

	var me struct {
		TotpEnabled bool `json:"totp_enabled"`
	}
	c.JSON(c.Get("/api/v1/auth/me"), &me)
	if !me.TotpEnabled {
		t.Fatal("me: totp_enabled false after confirm")
	}

	if resp := c.Post("/api/v1/auth/logout", nil); resp.StatusCode != 204 {
		t.Fatalf("logout %d", resp.StatusCode)
	}

	anon := h.Anonymous()
	var lb loginBody
	resp := anon.Post("/api/v1/auth/login", map[string]string{"username": "root", "password": "pass-123456"})
	if resp.StatusCode != 200 {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	anon.JSON(resp, &lb)
	if !lb.TotpRequired || lb.Challenge == "" {
		t.Fatalf("login body %+v, want a challenge", lb)
	}
	// The login response must not be a session: the second factor is still owed.
	if resp := anon.Get("/api/v1/auth/me"); resp.StatusCode != 401 {
		t.Fatalf("me before verify: %d, want 401", resp.StatusCode)
	}

	code, err := otptotp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	resp = anon.Post("/api/v1/auth/totp/verify", map[string]string{"challenge": lb.Challenge, "code": code})
	if resp.StatusCode != 200 {
		t.Fatalf("verify: %d", resp.StatusCode)
	}
	var vb loginBody
	anon.JSON(resp, &vb)
	if vb.Username != "root" || vb.Role != "owner" {
		t.Fatalf("verify body %+v", vb)
	}
	if resp := anon.Get("/api/v1/auth/me"); resp.StatusCode != 200 {
		t.Fatalf("me after verify: %d", resp.StatusCode)
	}

	// The login audit entry records that the second factor was a TOTP code.
	rows, err := h.Store.Q.ListAudit(context.Background(), db.ListAuditParams{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		var meta map[string]any
		_ = json.Unmarshal(r.Meta, &meta)
		if r.Action == "auth.login" && meta["totp"] == true {
			found = true
		}
	}
	if !found {
		t.Error("no auth.login audit entry with meta totp:true")
	}
}

func TestTOTPRecoveryCodeWorksOnce(t *testing.T) {
	h := apitest.New(t, apitest.WithTOTP())
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	_, recovery := enrol(t, c, "pass-123456")
	c.Post("/api/v1/auth/logout", nil).Body.Close() //nolint:errcheck

	use := func() (*http.Response, *apitest.Client) {
		anon := h.Anonymous()
		var lb loginBody
		anon.JSON(anon.Post("/api/v1/auth/login", map[string]string{"username": "root", "password": "pass-123456"}), &lb)
		return anon.Post("/api/v1/auth/totp/verify", map[string]string{"challenge": lb.Challenge, "recovery_code": recovery[0]}), anon
	}

	resp, anon := use()
	if resp.StatusCode != 200 {
		t.Fatalf("first recovery use: %d", resp.StatusCode)
	}
	if resp := anon.Get("/api/v1/auth/me"); resp.StatusCode != 200 {
		t.Fatalf("me after recovery login: %d", resp.StatusCode)
	}

	resp, _ = use()
	if resp.StatusCode != 401 {
		t.Fatalf("second use of the same recovery code: %d, want 401", resp.StatusCode)
	}
	code, _ := errCodeOf(t, resp)
	if code != "invalid_code" {
		t.Errorf("second use error code %q, want invalid_code", code)
	}
}

func TestTOTPVerifyWrongCodeCountsFailedLogin(t *testing.T) {
	h := apitest.New(t, apitest.WithTOTP())
	id := h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	enrol(t, c, "pass-123456")
	c.Post("/api/v1/auth/logout", nil).Body.Close() //nolint:errcheck

	anon := h.Anonymous()
	var lb loginBody
	anon.JSON(anon.Post("/api/v1/auth/login", map[string]string{"username": "root", "password": "pass-123456"}), &lb)

	resp := anon.Post("/api/v1/auth/totp/verify", map[string]string{"challenge": lb.Challenge, "code": "000001"})
	code, status := errCodeOf(t, resp)
	if status != 401 || code != "invalid_code" {
		t.Fatalf("wrong code: %d/%q, want 401/invalid_code", status, code)
	}
	u, err := h.Store.Q.GetAdmin(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if u.FailedLogins != 1 {
		t.Errorf("failed_logins = %d, want 1", u.FailedLogins)
	}
}

func TestTOTPVerifyRateLimitsWrongCodes(t *testing.T) {
	h := apitest.New(t, apitest.WithTOTP())
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	enrol(t, c, "pass-123456")
	c.Post("/api/v1/auth/logout", nil).Body.Close() //nolint:errcheck

	anon := h.Anonymous()
	var lb loginBody
	anon.JSON(anon.Post("/api/v1/auth/login", map[string]string{"username": "root", "password": "pass-123456"}), &lb)

	got401, got429 := 0, false
	for range 15 {
		_, status := errCodeOf(t, anon.Post("/api/v1/auth/totp/verify", map[string]string{"challenge": lb.Challenge, "code": "000001"}))
		switch status {
		case 401:
			got401++
		case 429:
			got429 = true
		}
		if got429 {
			break
		}
	}
	if !got429 {
		t.Fatalf("never rate-limited after %d wrong codes", got401)
	}
	if got401 < 5 {
		t.Errorf("rate limit kicked in after only %d attempts, expected the login allowance first", got401)
	}
}

func TestTOTPVerifyRejectsForgedAndExpiredChallenges(t *testing.T) {
	h := apitest.New(t, apitest.WithTOTP())
	uid := h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	secret, _ := enrol(t, c, "pass-123456")
	c.Post("/api/v1/auth/logout", nil).Body.Close() //nolint:errcheck

	code, _ := otptotp.GenerateCode(secret, time.Now())
	signer := crypto.NewSigner(h.Deps.Cfg.SessionSecret)
	soon := itoa(time.Now().Add(time.Minute).Unix())
	past := itoa(time.Now().Add(-time.Second).Unix())

	for name, challenge := range map[string]string{
		"empty":               "",
		"unsigned":            "totp:" + uid.String() + "|" + soon,
		"bad signature":       "totp:" + uid.String() + "|" + soon + ".AAAA",
		"unknown user":        signer.Sign("totp:00000000-0000-0000-0000-000000000000|" + soon),
		"no totp: prefix":     signer.Sign(uid.String() + "|" + soon),
		"wrong prefix":        signer.Sign("session:" + uid.String() + "|" + soon),
		"malformed payload":   signer.Sign("totp:nope"),
		"non-numeric expiry":  signer.Sign("totp:" + uid.String() + "|soon"),
		"expired but no uuid": signer.Sign("totp:not-a-uuid|" + past),
	} {
		resp := h.Anonymous().Post("/api/v1/auth/totp/verify", map[string]string{"challenge": challenge, "code": code})
		if errCode, status := errCodeOf(t, resp); status != 401 || errCode != "invalid_code" {
			t.Errorf("%s challenge: %d %s, want 401 invalid_code", name, status, errCode)
		}
	}

	stale := signer.Sign("totp:" + uid.String() + "|" + past)
	resp := h.Anonymous().Post("/api/v1/auth/totp/verify", map[string]string{"challenge": stale, "code": code})
	if errCode, status := errCodeOf(t, resp); status != 401 || errCode != "challenge_expired" {
		t.Fatalf("expired challenge: %d %s, want 401 challenge_expired", status, errCode)
	}
}

func TestTOTPChallengeIsNotASessionCookie(t *testing.T) {
	h := apitest.New(t, apitest.WithTOTP())
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	enrol(t, c, "pass-123456")
	c.Post("/api/v1/auth/logout", nil).Body.Close() //nolint:errcheck

	var lb loginBody
	anon := h.Anonymous()
	anon.JSON(anon.Post("/api/v1/auth/login", map[string]string{"username": "root", "password": "pass-123456"}), &lb)
	if lb.Challenge == "" {
		t.Fatal("no challenge")
	}
	forged := h.Anonymous()
	forged.SetHeader("Cookie", "tgwp_session="+lb.Challenge)
	if resp := forged.Get("/api/v1/auth/me"); resp.StatusCode != 401 {
		t.Fatalf("a challenge used as a session cookie: %d, want 401", resp.StatusCode)
	}
}

func TestTOTPDisableRequiresPasswordAndCode(t *testing.T) {
	h := apitest.New(t, apitest.WithTOTP())
	uid := h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	secret, _ := enrol(t, c, "pass-123456")

	code, _ := otptotp.GenerateCode(secret, time.Now())
	if resp := c.Post("/api/v1/auth/totp/disable", map[string]string{"password": "wrong-pass", "code": code}); resp.StatusCode != 422 {
		t.Fatalf("wrong password: %d, want 422", resp.StatusCode)
	}
	if resp := c.Post("/api/v1/auth/totp/disable", map[string]string{"password": "pass-123456", "code": "000001"}); resp.StatusCode != 422 {
		t.Fatalf("wrong code: %d, want 422", resp.StatusCode)
	}
	var me struct {
		TotpEnabled bool `json:"totp_enabled"`
	}
	c.JSON(c.Get("/api/v1/auth/me"), &me)
	if !me.TotpEnabled {
		t.Fatal("failed disable attempts must leave TOTP on")
	}

	code, _ = otptotp.GenerateCode(secret, time.Now())
	if resp := c.Post("/api/v1/auth/totp/disable", map[string]string{"password": "pass-123456", "code": code}); resp.StatusCode != 204 {
		t.Fatalf("disable: %d, want 204", resp.StatusCode)
	}
	c.JSON(c.Get("/api/v1/auth/me"), &me)
	if me.TotpEnabled {
		t.Fatal("me: totp_enabled still true after disable")
	}
	left, err := h.Store.Q.ListRecoveryCodes(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("%d recovery codes survived disable", len(left))
	}

	// With TOTP off, the password alone logs in again.
	var lb loginBody
	anon := h.Anonymous()
	anon.JSON(anon.Post("/api/v1/auth/login", map[string]string{"username": "root", "password": "pass-123456"}), &lb)
	if lb.TotpRequired {
		t.Error("login still demands a second factor after disable")
	}
}

func TestTOTPDisableAcceptsRecoveryCode(t *testing.T) {
	h := apitest.New(t, apitest.WithTOTP())
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	_, recovery := enrol(t, c, "pass-123456")

	if resp := c.Post("/api/v1/auth/totp/disable", map[string]string{"password": "pass-123456", "recovery_code": recovery[1]}); resp.StatusCode != 204 {
		t.Fatalf("disable with recovery code: %d, want 204", resp.StatusCode)
	}
}

func TestTOTPRoutes404WhenFeatureDisabled(t *testing.T) {
	h := apitest.New(t) // FEATURE_TOTP off
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	for _, path := range []string{"/api/v1/auth/totp/setup", "/api/v1/auth/totp/confirm", "/api/v1/auth/totp/disable"} {
		resp := c.Post(path, map[string]string{})
		code, status := errCodeOf(t, resp)
		if status != 404 || code != "feature_disabled" {
			t.Errorf("%s: %d/%q, want 404/feature_disabled", path, status, code)
		}
	}
	resp := h.Anonymous().Post("/api/v1/auth/totp/verify", map[string]string{"challenge": "x", "code": "000000"})
	if code, status := errCodeOf(t, resp); status != 404 || code != "feature_disabled" {
		t.Errorf("verify: %d/%q, want 404/feature_disabled", status, code)
	}
}

func TestViewerCanEnableTOTPForThemselves(t *testing.T) {
	h := apitest.New(t, apitest.WithTOTP())
	h.CreateAdmin("v", "pass-123456", "viewer")
	c := h.Login("v", "pass-123456")
	enrol(t, c, "pass-123456") // must not 403: the second factor is self-service for every role
}

func TestTOTPSecretIsEncryptedAtRest(t *testing.T) {
	h := apitest.New(t, apitest.WithTOTP())
	uid := h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	secret, recovery := enrol(t, c, "pass-123456")

	row, err := h.Store.Q.GetAdminTOTP(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(row.TotpSecretEnc), secret) {
		t.Error("totp_secret_enc holds the plaintext secret")
	}
	got, err := h.Box.DecryptString(row.TotpSecretEnc)
	if err != nil || got != secret {
		t.Fatalf("decrypt stored secret: %q %v", got, err)
	}
	if row.TotpPendingEnc != nil {
		t.Error("totp_pending_enc must be cleared once the enrolment is confirmed")
	}

	// Recovery codes are stored as sha256 hashes, never in the clear.
	stored, err := h.Store.Q.ListRecoveryCodes(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	hashes := map[string]bool{}
	for _, r := range stored {
		hashes[r.CodeHash] = true
	}
	for _, code := range recovery {
		if hashes[code] {
			t.Fatalf("recovery code %q stored in the clear", code)
		}
		if !hashes[crypto.HashToken(code)] {
			t.Fatalf("recovery code %q not stored as a sha256 hash", code)
		}
	}
}

func TestTOTPConfirmRequiresPendingEnrolment(t *testing.T) {
	h := apitest.New(t, apitest.WithTOTP())
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	if resp := c.Post("/api/v1/auth/totp/confirm", map[string]string{"password": "pass-123456", "code": "000000"}); resp.StatusCode != 409 {
		t.Fatalf("confirm without setup: %d, want 409", resp.StatusCode)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func TestTOTPConfirmRequiresThePasswordAndEndsOtherSessions(t *testing.T) {
	h := apitest.New(t, apitest.WithTOTP())
	uid := h.CreateAdmin("root", "pass-123456", "owner")
	enroller := h.Login("root", "pass-123456")
	other := h.Login("root", "pass-123456")

	var setup setupBody
	enroller.JSON(enroller.Post("/api/v1/auth/totp/setup", nil), &setup)
	if setup.Secret == "" {
		t.Fatal("setup returned no secret")
	}
	code, err := otptotp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	for name, body := range map[string]map[string]string{
		"no password":    {"code": code},
		"wrong password": {"password": "not-the-password", "code": code},
	} {
		if resp := enroller.Post("/api/v1/auth/totp/confirm", body); resp.StatusCode != 422 {
			t.Fatalf("%s: %d, want 422", name, resp.StatusCode)
		}
	}
	// Nothing was enabled by the refused attempts.
	row, err := h.Store.Q.GetAdminTOTP(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if row.TotpEnabled {
		t.Fatal("a confirm without the password enabled the second factor")
	}
	if resp := other.Get("/api/v1/auth/me"); resp.StatusCode != 200 {
		t.Fatalf("second session before confirm: %d, want 200", resp.StatusCode)
	}

	code, err = otptotp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var cb confirmBody
	resp := enroller.Post("/api/v1/auth/totp/confirm", map[string]string{"password": "pass-123456", "code": code})
	if resp.StatusCode != 200 {
		t.Fatalf("confirm with the password: %d, want 200", resp.StatusCode)
	}
	enroller.JSON(resp, &cb)
	if len(cb.RecoveryCodes) != 8 {
		t.Fatalf("got %d recovery codes, want 8", len(cb.RecoveryCodes))
	}

	if resp := other.Get("/api/v1/auth/me"); resp.StatusCode != 401 {
		t.Fatalf("second session after confirm: %d, want 401", resp.StatusCode)
	}
	// The tab that did the enrolling keeps working - it was handed a new cookie.
	if resp := enroller.Get("/api/v1/auth/me"); resp.StatusCode != 200 {
		t.Fatalf("enrolling session after confirm: %d, want 200", resp.StatusCode)
	}
}
