package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "tgwebproxy/proto/agent/v1"
)

type fakeExec struct {
	mu      sync.Mutex
	calls   []string
	failOn  string // substring of command that should fail every matching call
	failMsg string
	active  map[string]bool

	failNth      string
	failNthCount int
	nthSeen      map[string]int
}

func (f *fakeExec) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := name + " " + strings.Join(args, " ")
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	failStatic := f.failOn != "" && strings.Contains(cmd, f.failOn)
	failNth := false
	if f.failNth != "" && strings.Contains(cmd, f.failNth) {
		if f.nthSeen == nil {
			f.nthSeen = map[string]int{}
		}
		f.nthSeen[f.failNth]++
		failNth = f.nthSeen[f.failNth] == f.failNthCount
	}
	active := f.active
	f.mu.Unlock()

	if failStatic || failNth {
		return []byte(f.failMsg), errors.New("exit 1")
	}
	if name == "systemctl" && args[0] == "is-active" {
		if active == nil || active[args[1]] {
			return []byte("active\n"), nil
		}
		return []byte("inactive\n"), errors.New("exit 3")
	}
	return nil, nil
}

func (f *fakeExec) Start(_ context.Context, name string, args ...string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("2026-09-03T10:00:00+0000 host tproxy-server[1]: started\n")), nil
}

func (f *fakeExec) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeExec) has(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func testHandler(t *testing.T, ex *fakeExec, healthOK bool) (*Handler, Config) {
	t.Helper()
	dir := t.TempDir()
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthOK {
			w.WriteHeader(503)
			return
		}
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte("ok\n"))
		case "/readyz":
			_, _ = w.Write([]byte("ready\n"))
		case "/metrics":
			_, _ = w.Write([]byte("tproxy_sessions_live 3\n"))
		case "/stats":
			_, _ = w.Write([]byte("total_connections\t12\nactive_connections\t3\n"))
		}
	}))
	t.Cleanup(admin.Close)
	cfg := Config{
		StateDir: filepath.Join(dir, "state"), TProxyBin: "/usr/local/bin/tproxy-server",
		ConfigPath: filepath.Join(dir, "config.json"), ProfilesPath: filepath.Join(dir, "profiles.json"),
		MTProxyEnvPath: filepath.Join(dir, "mtproxy.env"), SiteDir: filepath.Join(dir, "site"),
		RelayAdminURL: admin.URL, MTProxyStatsURL: admin.URL + "/stats", TProxyVersion: "abc", HealthWait: 300 * time.Millisecond,
	}
	_ = os.WriteFile(cfg.ConfigPath, []byte(`{"public_hostname":"n.test"}`), 0o640)
	_ = os.WriteFile(cfg.ProfilesPath, []byte(`{"profiles":[{"name":"default","secret":"00000000000000000000000000000000","backend":"127.0.0.1:2398"}]}`), 0o400)
	_ = os.WriteFile(cfg.MTProxyEnvPath, []byte("MTPROXY_SECRET=00000000000000000000000000000000\nMTPROXY_WORKERS=2\nMTPROXY_MAX_CONNECTIONS=4096\n"), 0o640)
	_ = os.MkdirAll(cfg.SiteDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfg.SiteDir, "index.html"), []byte("old"), 0o644)
	return NewHandler(cfg, ex, slog.New(slog.DiscardHandler)), cfg
}

func TestApplyWritesFilesAndRestarts(t *testing.T) {
	ex := &fakeExec{}
	h, cfg := testHandler(t, ex, true)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true,
		Profiles: []*agentv1.Profile{
			{Name: "default", Secret: "00000000000000000000000000000000", Backend: "127.0.0.1:2398"},
			{Name: "kabc", Secret: "11111111111111111111111111111111", Backend: "127.0.0.1:2398", CarrierMode: "https-lanes", Limits: &agentv1.ProfileLimits{MaxSessions: 4}},
		},
		MtproxySecrets: []string{"00000000000000000000000000000000", "11111111111111111111111111111111"},
		Site:           &agentv1.SiteBundle{Files: []*agentv1.SiteFile{{Path: "index.html", Content: []byte("new")}, {Path: "a/s.css", Content: []byte("p{}")}}},
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	if !res.RestartedMtproxy || !res.RestartedRelay {
		t.Fatalf("expected both restarts: %+v", res)
	}
	if !ex.has("-check") || !ex.has("systemctl restart mtproxy") || !ex.has("systemctl restart tproxy-server") {
		t.Fatalf("missing commands: %v", ex.calls)
	}
	prof, _ := os.ReadFile(cfg.ProfilesPath)
	if !strings.Contains(string(prof), `"name":"kabc"`) || !strings.Contains(string(prof), `"max_sessions":4`) || strings.Contains(string(prof), "max_streams") {
		t.Fatalf("profiles.json wrong: %s", prof)
	}
	st, _ := os.Stat(cfg.ProfilesPath)
	if st.Mode().Perm() != 0o400 {
		t.Fatalf("profiles mode %o", st.Mode().Perm())
	}
	env, _ := os.ReadFile(cfg.MTProxyEnvPath)
	if !strings.Contains(string(env), "MTPROXY_SECRETS=-S 00000000000000000000000000000000 -S 11111111111111111111111111111111") || !strings.Contains(string(env), "MTPROXY_WORKERS=2") {
		t.Fatalf("env wrong: %s", env)
	}
	idx, _ := os.ReadFile(filepath.Join(cfg.SiteDir, "index.html"))
	css, _ := os.ReadFile(filepath.Join(cfg.SiteDir, "a", "s.css"))
	if string(idx) != "new" || string(css) != "p{}" {
		t.Fatal("site not deployed")
	}
	entries, _ := os.ReadDir(filepath.Join(cfg.StateDir, "backup"))
	if len(entries) != 1 {
		t.Fatalf("expected one backup dir, got %d", len(entries))
	}
}

func TestApplyCheckFailureLeavesFilesUntouched(t *testing.T) {
	ex := &fakeExec{failOn: "-check", failMsg: "profile \"x\": secret must decode to 16 bytes"}
	h, cfg := testHandler(t, ex, true)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true,
		Profiles:      []*agentv1.Profile{{Name: "x", Secret: "zz", Backend: "127.0.0.1:2398"}}, MtproxySecrets: []string{"zz"},
	})
	if res.Ok || !strings.Contains(res.Log, "16 bytes") {
		t.Fatalf("expected check failure, got %+v", res)
	}
	if ex.has("systemctl restart") {
		t.Fatal("must not restart after failed check")
	}
	prof, _ := os.ReadFile(cfg.ProfilesPath)
	if !strings.Contains(string(prof), `"name":"default"`) {
		t.Fatal("original profiles overwritten")
	}
}

func TestApplyRollsBackWhenHealthFails(t *testing.T) {
	ex := &fakeExec{}
	h, cfg := testHandler(t, ex, false)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles:  true,
		Profiles:       []*agentv1.Profile{{Name: "n", Secret: "11111111111111111111111111111111", Backend: "127.0.0.1:2398"}},
		MtproxySecrets: []string{"11111111111111111111111111111111"},
	})
	if res.Ok || !res.RolledBack {
		t.Fatalf("expected rollback, got %+v", res)
	}
	prof, _ := os.ReadFile(cfg.ProfilesPath)
	env, _ := os.ReadFile(cfg.MTProxyEnvPath)
	if !strings.Contains(string(prof), `"name":"default"`) || !strings.Contains(string(env), "MTPROXY_SECRET=00000000000000000000000000000000") {
		t.Fatalf("rollback did not restore files: %s / %s", prof, env)
	}
}

func TestApplySiteOnlyRestartsRelayOnly(t *testing.T) {
	ex := &fakeExec{}
	h, _ := testHandler(t, ex, true)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{Site: &agentv1.SiteBundle{Files: []*agentv1.SiteFile{{Path: "index.html", Content: []byte("x")}}}})
	if !res.Ok || res.RestartedMtproxy || !res.RestartedRelay {
		t.Fatalf("got %+v", res)
	}
}

func TestApplyUnchangedIsNoop(t *testing.T) {
	ex := &fakeExec{}
	h, _ := testHandler(t, ex, true)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles:  true,
		Profiles:       []*agentv1.Profile{{Name: "default", Secret: "00000000000000000000000000000000", Backend: "127.0.0.1:2398"}},
		MtproxySecrets: []string{"00000000000000000000000000000000"},
	})
	if !res.Ok || res.RestartedRelay || res.RestartedMtproxy {
		t.Fatalf("expected noop, got %+v", res)
	}
}

func TestRejectsSitePathTraversal(t *testing.T) {
	ex := &fakeExec{}
	h, _ := testHandler(t, ex, true)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{Site: &agentv1.SiteBundle{Files: []*agentv1.SiteFile{{Path: "../etc/x", Content: []byte("x")}}}})
	if res.Ok {
		t.Fatal("path traversal accepted")
	}
}

func TestRollbackReportsRestoreFailure(t *testing.T) {
	ex := &fakeExec{failNth: "systemctl restart tproxy-server", failNthCount: 2}
	h, cfg := testHandler(t, ex, false)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles:  true,
		Profiles:       []*agentv1.Profile{{Name: "n", Secret: "11111111111111111111111111111111", Backend: "127.0.0.1:2398"}},
		MtproxySecrets: []string{"11111111111111111111111111111111"},
	})
	if res.Ok {
		t.Fatalf("expected failure, got %+v", res)
	}
	if res.RolledBack {
		t.Fatalf("expected RolledBack=false when the rollback restart fails, got %+v", res)
	}
	if !strings.Contains(res.Log, "rollback FAILED") {
		t.Fatalf("expected log to call out rollback failure, got: %s", res.Log)
	}
	if !strings.Contains(res.Log, "rollback step failed") {
		t.Fatalf("expected log to name the failed step, got: %s", res.Log)
	}
	prof, _ := os.ReadFile(cfg.ProfilesPath)
	if !strings.Contains(string(prof), `"name":"default"`) {
		t.Fatalf("expected profiles.json restored despite restart failure: %s", prof)
	}
}

func TestApplyPrunesOldBackups(t *testing.T) {
	ex := &fakeExec{}
	h, cfg := testHandler(t, ex, true)
	backupRoot := filepath.Join(cfg.StateDir, "backup")

	// Eight applies, each changing the profile list so the apply is never a no-op.
	for i := range 8 {
		res := h.Apply(context.Background(), &agentv1.ApplyRequest{
			ApplyProfiles: true,
			Profiles: []*agentv1.Profile{
				{Name: "default", Secret: strings.Repeat(strconv.Itoa(i%10), 32), Backend: "127.0.0.1:2398"},
			},
			MtproxySecrets: []string{strings.Repeat(strconv.Itoa(i%10), 32)},
		})
		if !res.Ok {
			t.Fatalf("apply %d failed: %s", i, res.Log)
		}
	}

	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected the newest 5 backups to be kept, got %d: %v", len(entries), names)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.Name()] {
			t.Fatalf("duplicate backup dir name %s", e.Name())
		}
		seen[e.Name()] = true
	}
}

func TestSwapSiteDirRestoresOldOnCopyFailure(t *testing.T) {
	h, cfg := testHandler(t, &fakeExec{}, true)
	if err := h.swapSiteDir(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error copying from a missing source")
	}
	b, err := os.ReadFile(filepath.Join(cfg.SiteDir, "index.html"))
	if err != nil {
		t.Fatalf("previous site not restored: %v", err)
	}
	if string(b) != "old" {
		t.Fatalf("site content = %q, want the previous %q", b, "old")
	}
	for _, leftover := range []string{cfg.SiteDir + ".old", cfg.SiteDir + ".swap"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("%s left behind", leftover)
		}
	}
}

func TestApplyLogsChownFailure(t *testing.T) {
	ex := &fakeExec{failOn: "chown", failMsg: "chown: invalid group"}
	h, _ := testHandler(t, ex, true)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true,
		Profiles: []*agentv1.Profile{
			{Name: "default", Secret: "11111111111111111111111111111111", Backend: "127.0.0.1:2398"},
		},
	})
	if !res.Ok {
		t.Fatalf("apply must survive a chown failure: %s", res.Log)
	}
	if !strings.Contains(res.Log, "chown") || !strings.Contains(res.Log, "warning") {
		t.Fatalf("chown failure not reported in the apply log:\n%s", res.Log)
	}
}
