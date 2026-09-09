package admincli_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	otptotp "github.com/pquerna/otp/totp"

	"tgwebproxy/internal/admincli"
	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/store/db"
)

func enrol(t *testing.T, c *apitest.Client, password string) {
	t.Helper()
	var setup struct {
		Secret string `json:"secret"`
	}
	resp := c.Post("/api/v1/auth/totp/setup", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("setup: %d", resp.StatusCode)
	}
	c.JSON(resp, &setup)

	code, err := otptotp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var confirmed struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	resp = c.Post("/api/v1/auth/totp/confirm", map[string]string{"password": password, "code": code})
	if resp.StatusCode != 200 {
		t.Fatalf("confirm: %d", resp.StatusCode)
	}
	c.JSON(resp, &confirmed)
	if len(confirmed.RecoveryCodes) != 8 {
		t.Fatalf("got %d recovery codes, want 8", len(confirmed.RecoveryCodes))
	}
}

func TestResetTOTPClearsEnrolmentAndAudits(t *testing.T) {
	ctx := context.Background()
	h := apitest.New(t, apitest.WithTOTP())
	uid := h.CreateAdmin("root", "pass-123456", "owner")
	enrol(t, h.Login("root", "pass-123456"), "pass-123456")

	before, err := h.Store.Q.GetAdminTOTP(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if !before.TotpEnabled || len(before.TotpSecretEnc) == 0 {
		t.Fatalf("setup did not enrol: %+v", before)
	}

	u, err := admincli.ResetTOTP(ctx, h.Store, "root")
	if err != nil {
		t.Fatalf("ResetTOTP: %v", err)
	}
	if u.ID != uid {
		t.Errorf("returned admin %s, want %s", u.ID, uid)
	}

	after, err := h.Store.Q.GetAdminTOTP(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if after.TotpEnabled {
		t.Error("totp_enabled is still true")
	}
	if after.TotpSecretEnc != nil {
		t.Error("totp_secret_enc survived the reset")
	}
	if after.TotpPendingEnc != nil {
		t.Error("totp_pending_enc survived the reset")
	}
	codes, err := h.Store.Q.ListRecoveryCodes(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 0 {
		t.Errorf("%d recovery codes survived the reset", len(codes))
	}

	row, ok := findAudit(t, h, admincli.TOTPResetAction)
	if !ok {
		t.Fatal("no auth.totp_reset audit entry")
	}
	if row.AdminUserID.Valid {
		t.Errorf("admin_user_id = %s, want NULL (the actor is a shell, not an account)", row.AdminUserID.UUID)
	}
	if row.TargetType != "admin" || row.TargetID != uid.String() {
		t.Errorf("target = %s/%s, want admin/%s", row.TargetType, row.TargetID, uid)
	}
	if row.Ip != admincli.TOTPResetIP {
		t.Errorf("ip = %q, want %q", row.Ip, admincli.TOTPResetIP)
	}
	var meta map[string]any
	if err := json.Unmarshal(row.Meta, &meta); err != nil {
		t.Fatalf("meta %s: %v", row.Meta, err)
	}
	if meta["username"] != "root" {
		t.Errorf("meta = %v, want username root", meta)
	}
}

func TestResetTOTPUnknownUserChangesNothing(t *testing.T) {
	ctx := context.Background()
	h := apitest.New(t, apitest.WithTOTP())
	h.CreateAdmin("root", "pass-123456", "owner")

	_, err := admincli.ResetTOTP(ctx, h.Store, "nobody")
	if err == nil {
		t.Fatal("ResetTOTP on an unknown user must fail")
	}
	if !strings.Contains(err.Error(), "nobody") {
		t.Errorf("error %q should name the user it could not find", err)
	}
	if _, ok := findAudit(t, h, admincli.TOTPResetAction); ok {
		t.Error("a failed reset wrote an audit entry")
	}
}

func findAudit(t *testing.T, h *apitest.Harness, action string) (db.ListAuditRow, bool) {
	t.Helper()
	rows, err := h.Store.Q.ListAudit(context.Background(), db.ListAuditParams{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Action == action {
			return r, true
		}
	}
	return db.ListAuditRow{}, false
}
