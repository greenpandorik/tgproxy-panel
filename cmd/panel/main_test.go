package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tgwebproxy/internal/backup"
	"tgwebproxy/internal/config"
	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

func TestAdminCmdValidatesRoleBeforeDB(t *testing.T) {
	for _, bad := range []string{"Owner", "root", "", "admin ", "viewers"} {
		err := adminCmd(context.Background(), nil, []string{"create", "u", "pass-123456", bad})
		if err == nil {
			t.Errorf("role %q accepted", bad)
			continue
		}
		if !strings.Contains(err.Error(), "unknown role") {
			t.Errorf("role %q: got %v, want an unknown-role error", bad, err)
		}
	}
}

func TestAdminCmdUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"create"}, {"create", "u"}, {"delete", "u", "p"}} {
		if err := adminCmd(context.Background(), nil, args); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("args %v: got %v, want the usage error", args, err)
		}
	}
}

func TestDBRestoreRefusesWithoutYes(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DatabaseURL: "postgres://u:p@localhost/db"}
	err := dbCmd(context.Background(), cfg, nil, []string{"restore", "some.dump"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("got %v, want a refusal naming --yes", err)
	}
}

func TestDBUsage(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir()}
	for _, args := range [][]string{{}, {"restore"}, {"nonsense"}, {"restore", "a", "b", "--yes"}} {
		if err := dbCmd(context.Background(), cfg, nil, args); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("args %v: got %v, want the usage error", args, err)
		}
	}
}

func TestDBRestoreRefusesWhileThePanelHoldsTheDatabaseLock(t *testing.T) {
	holder := store.OpenTest(t)
	ctx := context.Background()

	lock, ok, err := holder.TryAdvisoryLock(ctx, panelAdvisoryLockID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the serve-style holder could not take a free lock")
	}
	// Released before any t.Cleanup runs: pgxpool.Close blocks on an acquired conn.
	defer lock.Release()

	caller := store.OpenTest(t)
	ran := false
	runner := &backup.Runner{
		DatabaseURL: "postgres://u:p@localhost/db", Dir: t.TempDir(),
		Exec: func(context.Context, string, ...string) ([]byte, error) { ran = true; return nil, nil },
	}
	cfg := config.Config{DataDir: t.TempDir(), DatabaseURL: "postgres://u:p@localhost/db"}

	err = dbRestore(ctx, cfg, runner, caller, []string{"some.dump", "--yes"})
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("got %v, want a refusal because the panel is running", err)
	}
	if ran {
		t.Fatal("pg_restore was invoked under a live panel")
	}
}

// The other half: with nothing holding the lock.
func TestDBRestoreProceedsWhenNoPanelHoldsTheDatabaseLock(t *testing.T) {
	st := store.OpenTest(t)
	ctx := context.Background()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "some.dump"), []byte("dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	var tool string
	runner := &backup.Runner{
		DatabaseURL: "postgres://u:p@localhost/db", Dir: dir,
		Exec: func(_ context.Context, name string, _ ...string) ([]byte, error) { tool = name; return nil, nil },
	}
	cfg := config.Config{DataDir: t.TempDir(), DatabaseURL: "postgres://u:p@localhost/db"}

	if err := dbRestore(ctx, cfg, runner, st, []string{"some.dump", "--yes"}); err != nil {
		t.Fatalf("restore with no panel running: %v", err)
	}
	if tool != "pg_restore" {
		t.Fatalf("ran %q, want pg_restore", tool)
	}
}

func TestKeysUsage(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), MasterKeyVersion: 1}
	for _, args := range [][]string{{}, {"rotate", "--bogus"}, {"nonsense"}} {
		if err := keysCmd(context.Background(), cfg, nil, args); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("args %v: got %v, want the usage error", args, err)
		}
	}
}

func TestKeysRotateRefusesWhileThePanelHoldsTheDatabaseLock(t *testing.T) {
	holder := store.OpenTest(t)
	ctx := context.Background()
	lock, ok, err := holder.TryAdvisoryLock(ctx, panelAdvisoryLockID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the serve-style holder could not take a free lock")
	}
	defer lock.Release()

	caller := store.OpenTest(t)
	cfg := config.Config{DataDir: t.TempDir(), MasterKeyVersion: 1, MasterKey: bytes.Repeat([]byte{0x44}, 32)}
	t.Setenv("MASTER_KEY_NEW", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x55}, 32)))

	err = keysCmd(ctx, cfg, caller, []string{"rotate"})
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("got %v, want a refusal because the panel is running", err)
	}
}

func TestKeysRotateRequiresMasterKeyNew(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), MasterKeyVersion: 1}
	t.Setenv("MASTER_KEY_NEW", "")
	err := keysCmd(context.Background(), cfg, nil, []string{"rotate"})
	if err == nil || !strings.Contains(err.Error(), "MASTER_KEY_NEW") {
		t.Fatalf("got %v, want a MASTER_KEY_NEW error", err)
	}
}

func TestKeysRotateValidatesMasterKeyNew(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), MasterKeyVersion: 1}
	for name, v := range map[string]string{
		"not base64": "not-valid-base64!!",
		"wrong size": "dG9vc2hvcnQ=", // base64("tooshort"), decodes to 8 bytes
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MASTER_KEY_NEW", v)
			err := keysCmd(context.Background(), cfg, nil, []string{"rotate"})
			if err == nil || !strings.Contains(err.Error(), "MASTER_KEY_NEW") {
				t.Fatalf("got %v, want a MASTER_KEY_NEW error", err)
			}
		})
	}
}

func TestKeysRotateRejectsBadCurrentMasterKey(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), MasterKeyVersion: 1, MasterKey: []byte("too short")}
	t.Setenv("MASTER_KEY_NEW", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	if err := keysCmd(context.Background(), cfg, nil, []string{"rotate"}); err == nil {
		t.Fatal("expected an error building the current-key Box")
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestKeysRotateDryRunAndHappyPathPrintNoKeyBytes(t *testing.T) {
	st := store.OpenTest(t)
	ctx := context.Background()

	currentKey := bytes.Repeat([]byte{0x11}, 32)
	newKey := bytes.Repeat([]byte{0x22}, 32)
	currentKeyB64 := base64.StdEncoding.EncodeToString(currentKey)
	newKeyB64 := base64.StdEncoding.EncodeToString(newKey)

	box, err := crypto.NewBox(1, map[int][]byte{1: currentKey})
	if err != nil {
		t.Fatal(err)
	}
	const plaintext = "0123456789abcdef0123456789abcdef"
	secretEnc, err := box.EncryptString(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	n, err := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "n", Hostname: "n.test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Q.CreateProfile(ctx, db.CreateProfileParams{
		NodeID: n.ID, Name: "default", SecretEnc: secretEnc, Backend: "127.0.0.1:2398", CarrierMode: "https", Limits: []byte("{}"),
	}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{DataDir: t.TempDir(), MasterKeyVersion: 1, MasterKey: currentKey}
	t.Setenv("MASTER_KEY_NEW", newKeyB64)

	assertNoKeyBytes := func(t *testing.T, out string) {
		t.Helper()
		if strings.Contains(out, currentKeyB64) {
			t.Fatalf("output contains the current MASTER_KEY's base64 form:\n%s", out)
		}
		if strings.Contains(out, newKeyB64) {
			t.Fatalf("output contains MASTER_KEY_NEW's base64 form:\n%s", out)
		}
	}

	var dryRunErr error
	dryRunOut := captureStdout(t, func() {
		dryRunErr = keysCmd(ctx, cfg, st, []string{"rotate", "--dry-run"})
	})
	if dryRunErr != nil {
		t.Fatalf("dry-run: %v", dryRunErr)
	}
	assertNoKeyBytes(t, dryRunOut)
	if !strings.Contains(dryRunOut, "profiles.secret_enc:          1") {
		t.Fatalf("dry-run output missing the pending profiles.secret_enc row, got:\n%s", dryRunOut)
	}

	// Dry-run must not have written anything: the row is still at v1.
	unchangedList, err := st.Q.ListNodeProfiles(ctx, n.ID)
	if err != nil || len(unchangedList) != 1 {
		t.Fatalf("list profiles: %v, %d rows", err, len(unchangedList))
	}
	if !bytes.Equal(unchangedList[0].SecretEnc, secretEnc) {
		t.Fatalf("--dry-run modified profiles.secret_enc")
	}

	var rotateErr error
	rotateOut := captureStdout(t, func() {
		rotateErr = keysCmd(ctx, cfg, st, []string{"rotate"})
	})
	if rotateErr != nil {
		t.Fatalf("rotate: %v", rotateErr)
	}
	assertNoKeyBytes(t, rotateOut)
	if !strings.Contains(rotateOut, "profiles.secret_enc:          1") {
		t.Fatalf("rotate output missing the re-encrypted profiles.secret_enc row, got:\n%s", rotateOut)
	}
	if !strings.Contains(rotateOut, "MASTER_KEY_VERSION=2") {
		t.Fatalf("rotate output missing the new version line, got:\n%s", rotateOut)
	}

	rotatedList, err := st.Q.ListNodeProfiles(ctx, n.ID)
	if err != nil || len(rotatedList) != 1 {
		t.Fatalf("list profiles: %v, %d rows", err, len(rotatedList))
	}
	rotated := rotatedList[0]
	if bytes.Equal(rotated.SecretEnc, secretEnc) {
		t.Fatalf("rotate did not change profiles.secret_enc")
	}
	toBox, err := crypto.NewBox(2, map[int][]byte{1: currentKey, 2: newKey})
	if err != nil {
		t.Fatal(err)
	}
	got, err := toBox.DecryptString(rotated.SecretEnc)
	if err != nil {
		t.Fatalf("decrypt rotated secret: %v", err)
	}
	if got != plaintext {
		t.Fatalf("rotated secret decrypts to %q, want %q", got, plaintext)
	}

	// Running rotate again is a documented no-op: every row is already at the target version.
	noopOut := captureStdout(t, func() {
		rotateErr = keysCmd(ctx, cfg, st, []string{"rotate", "--dry-run"})
	})
	if rotateErr != nil {
		t.Fatal(rotateErr)
	}
	assertNoKeyBytes(t, noopOut)
}

func TestKeysRotateRefusesWhenNewKeyEqualsCurrent(t *testing.T) {
	key := bytes.Repeat([]byte{0x33}, 32)
	cfg := config.Config{DataDir: t.TempDir(), MasterKeyVersion: 1, MasterKey: key}
	t.Setenv("MASTER_KEY_NEW", base64.StdEncoding.EncodeToString(key))
	err := keysCmd(context.Background(), cfg, nil, []string{"rotate"})
	if err == nil || !strings.Contains(err.Error(), "must not equal the current MASTER_KEY") {
		t.Fatalf("got %v, want a refusal naming the equal-key case", err)
	}
}
