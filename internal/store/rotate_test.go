package store_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

type telegramAlertsFixture struct {
	Enabled     bool   `json:"enabled"`
	BotTokenEnc string `json:"bot_token_enc"`
	ChatID      string `json:"chat_id"`
}

type rotateFixture struct {
	nodeID    uuid.UUID
	profileID uuid.UUID
	keyID     uuid.UUID
	adminID   uuid.UUID

	profileSecret string
	keySecret     string
	totpSecret    string
	totpPending   string
	botToken      string
}

func seedRotateFixture(t *testing.T, st *store.Store, box *crypto.Box) rotateFixture {
	t.Helper()
	ctx := context.Background()

	n, err := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "n", Hostname: "n.test"})
	if err != nil {
		t.Fatal(err)
	}

	f := rotateFixture{
		nodeID:        n.ID,
		profileSecret: "0123456789abcdef0123456789abcdef",
		keySecret:     "fedcba9876543210fedcba9876543210",
		totpSecret:    "JBSWY3DPEHPK3PXP",
		totpPending:   "KRSXG5CTMVRXEZLU",
		botToken:      "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
	}

	profSecretEnc, err := box.EncryptString(f.profileSecret)
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.Q.CreateProfile(ctx, db.CreateProfileParams{
		NodeID: n.ID, Name: "default", SecretEnc: profSecretEnc, Backend: "127.0.0.1:2398", CarrierMode: "https", Limits: []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.profileID = p.ID

	keySecretEnc, err := box.EncryptString(f.keySecret)
	if err != nil {
		t.Fatal(err)
	}
	k, err := st.Q.CreateKey(ctx, db.CreateKeyParams{
		Label: "k", Type: db.KeyTypeSHARED, SecretEnc: keySecretEnc, CarrierMode: "https", Limits: []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.keyID = k.ID

	admin, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: "root", PasswordHash: "x", Role: db.AdminRoleOwner})
	if err != nil {
		t.Fatal(err)
	}
	f.adminID = admin.ID
	totpSecretEnc, err := box.EncryptString(f.totpSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Q.SetAdminTOTP(ctx, db.SetAdminTOTPParams{ID: admin.ID, TotpSecretEnc: totpSecretEnc, TotpEnabled: true}); err != nil {
		t.Fatal(err)
	}
	totpPendingEnc, err := box.EncryptString(f.totpPending)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Q.SetAdminTOTPPending(ctx, db.SetAdminTOTPPendingParams{ID: admin.ID, TotpPendingEnc: totpPendingEnc}); err != nil {
		t.Fatal(err)
	}

	botTokenEnc, err := box.EncryptString(f.botToken)
	if err != nil {
		t.Fatal(err)
	}
	stored := telegramAlertsFixture{Enabled: true, BotTokenEnc: base64.StdEncoding.EncodeToString(botTokenEnc), ChatID: "-100123"}
	raw, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Q.UpsertSetting(ctx, db.UpsertSettingParams{Key: "telegram_alerts", Value: raw}); err != nil {
		t.Fatal(err)
	}

	return f
}

func rotateBoxes(t *testing.T) (from, to *crypto.Box) {
	t.Helper()
	k1 := bytes.Repeat([]byte{1}, 32)
	k2 := bytes.Repeat([]byte{2}, 32)
	from, err := crypto.NewBox(1, map[int][]byte{1: k1, 2: k2})
	if err != nil {
		t.Fatal(err)
	}
	to, err = crypto.NewBox(2, map[int][]byte{1: k1, 2: k2})
	if err != nil {
		t.Fatal(err)
	}
	return from, to
}

// getProfile fetches the single seeded profile on a node.
func getProfile(t *testing.T, st *store.Store, nodeID uuid.UUID) db.Profile {
	t.Helper()
	rows, err := st.Q.ListNodeProfiles(context.Background(), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(rows))
	}
	return rows[0]
}

func assertVersion(t *testing.T, box *crypto.Box, blob []byte, want int) {
	t.Helper()
	if got := box.Version(blob); got != want {
		t.Fatalf("version = %d, want %d", got, want)
	}
}

func TestRotateReencryptsEveryColumn(t *testing.T) {
	st := store.OpenTest(t)
	from, to := rotateBoxes(t)
	f := seedRotateFixture(t, st, from)
	ctx := context.Background()

	report, err := store.Rotate(ctx, st, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if report != (store.Report{Profiles: 1, Keys: 1, TOTPSecrets: 1, TOTPPending: 1, Settings: 1}) {
		t.Fatalf("report = %+v", report)
	}

	prof := getProfile(t, st, f.nodeID)
	assertVersion(t, to, prof.SecretEnc, 2)
	got, err := to.DecryptString(prof.SecretEnc)
	if err != nil || got != f.profileSecret {
		t.Fatalf("profile secret got %q err %v", got, err)
	}

	key, err := st.Q.GetKey(ctx, f.keyID)
	if err != nil {
		t.Fatal(err)
	}
	assertVersion(t, to, key.SecretEnc, 2)
	got, err = to.DecryptString(key.SecretEnc)
	if err != nil || got != f.keySecret {
		t.Fatalf("key secret got %q err %v", got, err)
	}

	totp, err := st.Q.GetAdminTOTP(ctx, f.adminID)
	if err != nil {
		t.Fatal(err)
	}
	assertVersion(t, to, totp.TotpSecretEnc, 2)
	got, err = to.DecryptString(totp.TotpSecretEnc)
	if err != nil || got != f.totpSecret {
		t.Fatalf("totp secret got %q err %v", got, err)
	}
	assertVersion(t, to, totp.TotpPendingEnc, 2)
	got, err = to.DecryptString(totp.TotpPendingEnc)
	if err != nil || got != f.totpPending {
		t.Fatalf("totp pending got %q err %v", got, err)
	}

	raw, err := st.Q.GetSetting(ctx, "telegram_alerts")
	if err != nil {
		t.Fatal(err)
	}
	var stored telegramAlertsFixture
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	blob, err := base64.StdEncoding.DecodeString(stored.BotTokenEnc)
	if err != nil {
		t.Fatal(err)
	}
	assertVersion(t, to, blob, 2)
	got, err = to.DecryptString(blob)
	if err != nil || got != f.botToken {
		t.Fatalf("bot token got %q err %v", got, err)
	}
	// Unrelated fields must survive the rewrite.
	if !stored.Enabled || stored.ChatID != "-100123" {
		t.Fatalf("stored = %+v", stored)
	}

	// Running again is a no-op: every row is already at the target version.
	report2, err := store.Rotate(ctx, st, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if report2 != (store.Report{}) {
		t.Fatalf("second run report = %+v, want all zero", report2)
	}
}

func TestRotateSkipsNullColumns(t *testing.T) {
	st := store.OpenTest(t)
	from, to := rotateBoxes(t)
	ctx := context.Background()

	if _, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: "bare", PasswordHash: "x", Role: db.AdminRoleViewer}); err != nil {
		t.Fatal(err)
	}

	report, err := store.Rotate(ctx, st, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if report != (store.Report{}) {
		t.Fatalf("report = %+v, want all zero", report)
	}
}

func TestRotateRollsBackOnCorruptBlob(t *testing.T) {
	st := store.OpenTest(t)
	from, to := rotateBoxes(t)
	f := seedRotateFixture(t, st, from)
	ctx := context.Background()

	badBlob, err := from.EncryptString(f.keySecret)
	if err != nil {
		t.Fatal(err)
	}
	badBlob[len(badBlob)-1] ^= 0xff
	if err := st.Q.SetKeySecret(ctx, db.SetKeySecretParams{ID: f.keyID, SecretEnc: badBlob}); err != nil {
		t.Fatal(err)
	}
	// SetKeySecret resets status to pending; irrelevant to this test.

	before := getProfile(t, st, f.nodeID)

	if _, err := store.Rotate(ctx, st, from, to); err == nil {
		t.Fatal("expected an error from the corrupted blob")
	}

	after := getProfile(t, st, f.nodeID)
	if !bytes.Equal(before.SecretEnc, after.SecretEnc) {
		t.Fatal("profile row changed despite the rollback")
	}
	assertVersion(t, from, after.SecretEnc, 1)

	key, err := st.Q.GetKey(ctx, f.keyID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key.SecretEnc, badBlob) {
		t.Fatal("key row changed despite the rollback")
	}
}

func TestCountPendingDryRun(t *testing.T) {
	st := store.OpenTest(t)
	from, to := rotateBoxes(t)
	seedRotateFixture(t, st, from)
	ctx := context.Background()

	report, err := store.CountPending(ctx, st, to)
	if err != nil {
		t.Fatal(err)
	}
	if report != (store.Report{Profiles: 1, Keys: 1, TOTPSecrets: 1, TOTPPending: 1, Settings: 1}) {
		t.Fatalf("report = %+v", report)
	}

	if _, err := store.Rotate(ctx, st, from, to); err != nil {
		t.Fatal(err)
	}
	report2, err := store.CountPending(ctx, st, to)
	if err != nil {
		t.Fatal(err)
	}
	if report2 != (store.Report{}) {
		t.Fatalf("after rotate report = %+v, want all zero", report2)
	}
}
