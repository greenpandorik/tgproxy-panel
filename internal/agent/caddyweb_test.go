package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const installedCaddyfile = "{\n\temail ops@example.com\n\tservers {\n\t\ttrusted_proxies static private_ranges\n\t}\n}\n\n" +
	"n1.example.com {\n\tencode zstd gzip\n\treverse_proxy 127.0.0.1:18080 {\n\t\theader_up X-Forwarded-For {remote_host}\n\t}\n}\n"

func caddyHandler(t *testing.T, ex *fakeExec) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(path, []byte(installedCaddyfile), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewHandler(Config{Engine: EngineTelemt, StateDir: dir}, ex, slog.New(slog.DiscardHandler)), path
}

func TestCaddyWebTuningRewritesTheInstallerCaddyfile(t *testing.T) {
	ex := &fakeExec{}
	h, path := caddyHandler(t, ex)
	if err := h.ensureCaddyWebTuning(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	for _, want := range []string{"email ops@example.com", "n1.example.com {", "protocols h1 h2", "flush_interval -1", "stream_close_delay 5m", "header_up X-Forwarded-For {remote_host}"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "encode") {
		t.Errorf("compression left in:\n%s", got)
	}
	if len(ex.calls) != 2 || !strings.HasPrefix(ex.calls[0], "caddy validate --config ") || ex.calls[1] != "systemctl reload caddy" {
		t.Fatalf("calls %v", ex.calls)
	}
	if _, err := os.Stat(path + ".tgwp-new"); !os.IsNotExist(err) {
		t.Fatal("the candidate file was left behind")
	}

	ex.calls = nil
	if err := h.ensureCaddyWebTuning(context.Background(), path); err != nil || len(ex.calls) != 0 {
		t.Fatalf("a tuned Caddyfile must be left alone: %v %v", ex.calls, err)
	}
}

func TestCaddyWebTuningLeavesAnEditedCaddyfileAlone(t *testing.T) {
	ex := &fakeExec{}
	h, path := caddyHandler(t, ex)
	edited := strings.Replace(installedCaddyfile, "encode zstd gzip", "encode gzip\n\tlog", 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.ensureCaddyWebTuning(context.Background(), path); err != nil || len(ex.calls) != 0 {
		t.Fatalf("an operator's own Caddyfile must not be touched: %v %v", ex.calls, err)
	}
	if b, _ := os.ReadFile(path); string(b) != edited {
		t.Fatal("the file changed")
	}
}

func TestCaddyWebTuningKeepsTheOldFileWhenValidationFails(t *testing.T) {
	ex := &fakeExec{failOn: "caddy validate", failMsg: "bad"}
	h, path := caddyHandler(t, ex)
	if err := h.ensureCaddyWebTuning(context.Background(), path); err == nil {
		t.Fatal("want the validation error")
	}
	if b, _ := os.ReadFile(path); string(b) != installedCaddyfile {
		t.Fatal("the old Caddyfile was replaced")
	}
	if slices.Contains(ex.calls, "systemctl reload caddy") {
		t.Fatal("caddy was reloaded with an invalid file")
	}
}

func TestCaddyWebTuningRestoresTheOldFileWhenReloadFails(t *testing.T) {
	ex := &fakeExec{failOn: "systemctl reload caddy", failMsg: "boom"}
	h, path := caddyHandler(t, ex)
	if err := h.ensureCaddyWebTuning(context.Background(), path); err == nil {
		t.Fatal("want the reload error")
	}
	if b, _ := os.ReadFile(path); string(b) != installedCaddyfile {
		t.Fatal("the old Caddyfile was not restored")
	}
}

func TestCaddyWebTuningSkipsTProxyNodes(t *testing.T) {
	ex := &fakeExec{}
	dir := t.TempDir()
	path := filepath.Join(dir, "Caddyfile")
	_ = os.WriteFile(path, []byte(installedCaddyfile), 0o644)
	h := NewHandler(Config{Engine: EngineTProxy, StateDir: dir}, ex, slog.New(slog.DiscardHandler))
	if err := h.ensureCaddyWebTuning(context.Background(), path); err != nil || len(ex.calls) != 0 {
		t.Fatal("tproxy nodes are not touched")
	}
}
