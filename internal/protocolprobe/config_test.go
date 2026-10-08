package protocolprobe

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigProtectsSecrets(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		mode       os.FileMode
		valid      bool
	}{
		{"valid", `{"faketls":{"secret":"000102030405060708090a0b0c0d0e0f","sni":"cover.example.com"},"web":{"secret":"dd000102030405060708090a0b0c0d0e0f"}}`, 0o600, true},
		{"unconfigured", `{}`, 0o600, true},
		{"null", `null`, 0o600, false},
		{"public file", `{}`, 0o644, false},
		{"wrong secret", `{"web":{"secret":"private-invalid-secret"}}`, 0o600, false},
		{"unknown", `{"secret":"private-invalid-secret"}`, 0o600, false},
		{"trailing", `{} {}`, 0o600, false},
		{"bad timeout", `{"timeout_seconds":61}`, 0o600, false},
		{"bad dc", `{"dc":9}`, 0o600, false},
		{"fake without sni", `{"faketls":{"secret":"000102030405060708090a0b0c0d0e0f"}}`, 0o600, false},
		{"ee web", `{"web":{"secret":"ee000102030405060708090a0b0c0d0e0f"}}`, 0o600, false},
		{"aliased base path", `{"web":{"secret":"000102030405060708090a0b0c0d0e0f","base_path":"a//b"}}`, 0o600, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if e := os.WriteFile(path, []byte(tc.body), tc.mode); e != nil {
				t.Fatal(e)
			}
			_, e := LoadConfig(path)
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, e)
			}
			if e != nil && strings.Contains(e.Error(), "private-invalid-secret") {
				t.Fatal("secret leaked")
			}
		})
	}
}

func TestLoadConfigRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if e := os.WriteFile(path, []byte(`{}`), 0o600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(dir, "link")
	if e := os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadConfig(link); e == nil {
		t.Fatal("accepted symlink")
	}
}

func TestUnconfiguredTransportsStayNotRun(t *testing.T) {
	result := Check(t.Context(), "127.0.0.1", Config{})
	if result.FakeTLS.Status != "not_run" || result.WEB.Status != "not_run" {
		t.Fatalf("%+v", result)
	}
}

func TestAuthenticatedAttemptsMarkMethodButInvalidConfigurationDoesNot(t *testing.T) {
	cfg := Config{FakeTLS: &Transport{Secret: "00112233445566778899aabbccddeeff", SNI: "example.org", Port: 1}, TimeoutSeconds: 1}
	result := Check(context.Background(), "127.0.0.1", cfg)
	raw, _ := json.Marshal(result)
	if !strings.Contains(string(raw), `"method":"authenticated_mtproto"`) {
		t.Fatalf("actual configured attempt lacks marker: %s", raw)
	}
	result = Check(context.Background(), "invalid/host", cfg)
	raw, _ = json.Marshal(result)
	if strings.Contains(string(raw), `"method"`) {
		t.Fatalf("invalid local configuration marked as a protocol attempt: %s", raw)
	}
}
