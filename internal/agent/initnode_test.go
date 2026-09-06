package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitNode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	envPath := filepath.Join(dir, "mtproxy.env")
	dropin := filepath.Join(dir, "secrets.conf")
	_ = os.WriteFile(cfgPath, []byte(`{"public_hostname":"n.test","listen":"127.0.0.1:8080","limits":{"max_sessions_global":64}}`), 0o640)
	_ = os.WriteFile(envPath, []byte("MTPROXY_SECRET=00000000000000000000000000000000\nMTPROXY_WORKERS=1\nMTPROXY_MAX_CONNECTIONS=4096\n"), 0o640)
	if err := InitNode(cfgPath, envPath, dropin, 128); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	raw, _ := os.ReadFile(cfgPath)
	_ = json.Unmarshal(raw, &cfg)
	limits := cfg["limits"].(map[string]any)
	if limits["max_profiles"].(float64) != 128 || limits["max_sessions_global"].(float64) != 64 || cfg["listen"] != "127.0.0.1:8080" {
		t.Fatalf("config not merged: %s", raw)
	}
	env, _ := os.ReadFile(envPath)
	if !strings.Contains(string(env), "MTPROXY_SECRETS=-S 00000000000000000000000000000000") {
		t.Fatalf("env: %s", env)
	}
	d, _ := os.ReadFile(dropin)
	if !strings.Contains(string(d), "ExecStart=\n") || !strings.Contains(string(d), "$MTPROXY_SECRETS") {
		t.Fatalf("dropin: %s", d)
	}
}
