package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeExec answers the systemctl/journalctl/version calls the upgrade makes, and records them
// so a test can assert what the node was actually told to do.
type upgradeExec struct {
	mu    sync.Mutex
	calls []string
	fn    func(name string, args []string) ([]byte, error)
}

func (f *upgradeExec) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	f.mu.Unlock()
	if f.fn == nil {
		return nil, nil
	}
	return f.fn(name, args)
}

func (f *upgradeExec) Start(context.Context, string, ...string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not used")
}

func (f *upgradeExec) ranCmd(want string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == want {
			return true
		}
	}
	return false
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestNormalizeAndCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		installed, wanted string
		same              bool
	}{
		{"3.5.6", "3.5.6", true},
		{"telemt 3.5.6", "3.5.6", true},
		{"v3.5.6\n", "3.5.6", true},
		{"3.5.5", "3.5.6", false},
		// A node ahead of the pin is out of date too: the pin is the truth, so the same
		// command performs the documented downgrade.
		{"3.6.0", "3.5.6", false},
		{"", "3.5.6", false},
	} {
		if got := sameVersion(tc.installed, tc.wanted); got != tc.same {
			t.Errorf("sameVersion(%q, %q) = %v, want %v", tc.installed, tc.wanted, got, tc.same)
		}
	}
}

func TestParseVersionOutput(t *testing.T) {
	for in, want := range map[string]string{
		"telemt 3.5.6":             "3.5.6",
		"telemt version 3.5.6-rc1": "3.5.6-rc1",
		"3.5.6\n":                  "3.5.6",
		"unknown":                  "",
	} {
		if got := ParseVersionOutput(in); got != want {
			t.Errorf("ParseVersionOutput(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadEnvFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.env")
	if err := os.WriteFile(p, []byte("# comment\nTGWP_PANEL_URL=https://panel.test\n\nTGWP_TOKEN=secret-token\nTGWP_ENGINE=telemt\nnot a pair\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := ReadEnvFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if env["TGWP_PANEL_URL"] != "https://panel.test" || env["TGWP_TOKEN"] != "secret-token" || env["TGWP_ENGINE"] != "telemt" {
		t.Fatalf("env = %v", env)
	}
	if len(env) != 3 {
		t.Fatalf("comment/blank/garbage lines must be skipped: %v", env)
	}
}

func telemtManifest(agentSHA string) UpgradeManifest {
	return UpgradeManifest{
		Engine: EngineTelemt,
		Telemt: &UpgradeArtifact{Version: "3.5.6", SHA256: strings.Repeat("a", 64), URL: "https://example.test/telemt.tar.gz"},
		Agent:  UpgradeArtifact{Version: "0.2.0", SHA256: agentSHA, URL: "https://panel.test/api/v1/install/agent/linux-amd64"},
	}
}

func TestBuildUpgradePlan(t *testing.T) {
	m := telemtManifest(strings.Repeat("b", 64))

	t.Run("both out of date", func(t *testing.T) {
		plan := BuildUpgradePlan(m, Installed{ComponentTelemt: "3.5.5", ComponentAgent: "0.1.0"}, UpgradeScope{})
		got := plan.Changes()
		if len(got) != 2 || got[0].Name != ComponentTelemt || got[1].Name != ComponentAgent {
			t.Fatalf("changes = %+v; telemt must come before the agent", got)
		}
		if got[0].Reason != "3.5.5 → 3.5.6" || got[1].Reason != "0.1.0 → 0.2.0" {
			t.Errorf("reasons %q / %q", got[0].Reason, got[1].Reason)
		}
	})

	t.Run("already current", func(t *testing.T) {
		plan := BuildUpgradePlan(m, Installed{ComponentTelemt: "telemt 3.5.6", ComponentAgent: "0.2.0"}, UpgradeScope{})
		if len(plan.Changes()) != 0 {
			t.Fatalf("changes = %+v", plan.Changes())
		}
		for _, c := range plan.Components {
			if !strings.HasPrefix(c.Reason, "up to date") {
				t.Errorf("%s: %q", c.Name, c.Reason)
			}
		}
	})

	t.Run("unknown installed version is out of date", func(t *testing.T) {
		plan := BuildUpgradePlan(m, Installed{ComponentAgent: "0.2.0"}, UpgradeScope{})
		got := plan.Changes()
		if len(got) != 1 || got[0].Name != ComponentTelemt || !strings.Contains(got[0].Reason, "unknown") {
			t.Fatalf("changes = %+v", got)
		}
	})

	t.Run("scope limits the plan", func(t *testing.T) {
		installed := Installed{ComponentTelemt: "3.5.5", ComponentAgent: "0.1.0"}
		only := BuildUpgradePlan(m, installed, UpgradeScope{Agent: true}).Changes()
		if len(only) != 1 || only[0].Name != ComponentAgent {
			t.Fatalf("--agent changes = %+v", only)
		}
		only = BuildUpgradePlan(m, installed, UpgradeScope{Telemt: true}).Changes()
		if len(only) != 1 || only[0].Name != ComponentTelemt {
			t.Fatalf("--telemt changes = %+v", only)
		}
		// Both flags together is the same as neither.
		if got := BuildUpgradePlan(m, installed, UpgradeScope{Telemt: true, Agent: true}).Changes(); len(got) != 2 {
			t.Fatalf("--telemt --agent changes = %+v", got)
		}
	})

	t.Run("no checksum is never installed", func(t *testing.T) {
		unpinned := m
		unpinned.Agent.SHA256 = ""
		plan := BuildUpgradePlan(unpinned, Installed{ComponentTelemt: "3.5.6", ComponentAgent: "0.1.0"}, UpgradeScope{})
		if len(plan.Changes()) != 0 {
			t.Fatalf("an artifact with no sha256 must not be planned: %+v", plan.Changes())
		}
		if !strings.Contains(plan.Components[1].Reason, "unverified") {
			t.Errorf("reason = %q", plan.Components[1].Reason)
		}
	})

	t.Run("tproxy node reports its commit and never upgrades it", func(t *testing.T) {
		tp := UpgradeManifest{
			Engine: EngineTProxy,
			TProxy: &UpgradeTProxy{Commit: strings.Repeat("c", 40), Repo: "https://example.test/tproxy.git"},
			Agent:  UpgradeArtifact{Version: "0.2.0", SHA256: strings.Repeat("b", 64), URL: "https://panel.test/agent"},
		}
		plan := BuildUpgradePlan(tp, Installed{"tproxy-server": strings.Repeat("d", 40), ComponentAgent: "0.2.0"}, UpgradeScope{})
		if got := plan.Changes(); len(got) != 0 {
			t.Fatalf("nothing is upgradable on a current tproxy node: %+v", got)
		}
		if !strings.Contains(plan.Components[0].Reason, "re-running its install command") {
			t.Errorf("reason = %q", plan.Components[0].Reason)
		}
	})
}

func TestFetchUpgradeManifest(t *testing.T) {
	var gotAuth string
	code := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != UpgradePath {
			t.Errorf("path = %s", r.URL.Path)
		}
		if code != http.StatusOK {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(telemtManifest(strings.Repeat("b", 64)))
	}))
	defer srv.Close()

	m, err := FetchUpgradeManifest(context.Background(), srv.Client(), srv.URL+"/", "node-token")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer node-token" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if m.Engine != EngineTelemt || m.Telemt == nil || m.Telemt.Version != "3.5.6" || m.Agent.Version != "0.2.0" {
		t.Fatalf("manifest = %+v", m)
	}

	code = http.StatusUnauthorized
	_, err = FetchUpgradeManifest(context.Background(), srv.Client(), srv.URL, "node-token")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("401 must be reported as a rejected token: %v", err)
	}
}

// stubPanel serves an upgrade manifest and the agent binary it names.
type stubPanel struct {
	srv      *httptest.Server
	manifest UpgradeManifest
	// agentBody is what /agent serves; declaredSHA is what the manifest claims it is.
	agentBody []byte
}

func newStubPanel(t *testing.T, agentBody []byte, declaredSHA string) *stubPanel {
	t.Helper()
	s := &stubPanel{agentBody: agentBody}
	mux := http.NewServeMux()
	mux.HandleFunc("/agent", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(s.agentBody) })
	mux.HandleFunc(UpgradePath, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer node-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(s.manifest)
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	s.manifest = UpgradeManifest{
		Engine: EngineTProxy,
		TProxy: &UpgradeTProxy{Commit: strings.Repeat("c", 40)},
		Agent:  UpgradeArtifact{Version: "0.2.0", SHA256: declaredSHA, URL: s.srv.URL + "/agent"},
	}
	return s
}

// upgradeBench is a node in a temp directory: an env file, an installed agent binary, and a
// fake systemd.
type upgradeBench struct {
	dir      string
	envPath  string
	agentBin string
	exec     *upgradeExec
	out      bytes.Buffer
}

func newUpgradeBench(t *testing.T, panelURL string, installedAgent []byte) *upgradeBench {
	t.Helper()
	b := &upgradeBench{dir: t.TempDir()}
	b.envPath = filepath.Join(b.dir, "agent.env")
	b.agentBin = filepath.Join(b.dir, "tgwp-agent")
	if err := os.WriteFile(b.envPath, []byte("TGWP_PANEL_URL="+panelURL+"\nTGWP_TOKEN=node-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.agentBin, installedAgent, 0o755); err != nil {
		t.Fatal(err)
	}
	return b
}

// options returns UpgradeOptions wired to the bench: no terminal, no real sleeping.
func (b *upgradeBench) options() UpgradeOptions {
	no := false
	return UpgradeOptions{
		EnvPath: b.envPath, AgentBin: b.agentBin, TelemtBin: filepath.Join(b.dir, "telemt"),
		Out: &b.out, Err: &b.out, TTY: &no, Exec: b.exec,
		Env: func(string) string { return "" }, HealthWait: 3 * time.Second, Sleep: func(time.Duration) {},
	}
}

func (b *upgradeBench) agentContent(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(b.agentBin)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestUpgradeChecksumMismatchAborts is the important one: a download whose sha256 does not
// match the panel's pin must never reach the target, and nothing may be restarted.
func TestUpgradeChecksumMismatchAborts(t *testing.T) {
	panel := newStubPanel(t, []byte("tampered binary"), sha256Hex([]byte("the binary the panel pinned")))
	b := newUpgradeBench(t, panel.srv.URL, []byte("old agent"))
	b.exec = &upgradeExec{fn: func(name string, args []string) ([]byte, error) {
		if name == b.agentBin && len(args) == 1 && args[0] == "version" {
			return []byte("0.1.0\n"), nil
		}
		return nil, nil
	}}
	o := b.options()
	o.Yes = true

	err := RunUpgrade(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want a checksum mismatch", err)
	}
	if got := b.agentContent(t); got != "old agent" {
		t.Fatalf("the installed binary was replaced from an unverified download: %q", got)
	}
	if _, err := os.Stat(b.agentBin + ".prev"); err == nil {
		t.Error("nothing should have been backed up: the abort happens before anything is touched")
	}
	if b.exec.ranCmd("systemctl restart tgwp-agent") {
		t.Error("the unit must not be restarted after a failed verification")
	}
	if !strings.Contains(b.out.String(), "Nothing was replaced") {
		t.Errorf("output does not say nothing was replaced:\n%s", b.out.String())
	}
}

func TestUpgradeAgentSucceeds(t *testing.T) {
	newBin := []byte("new agent binary")
	panel := newStubPanel(t, newBin, sha256Hex(newBin))
	b := newUpgradeBench(t, panel.srv.URL, []byte("old agent"))
	b.exec = &upgradeExec{fn: func(name string, args []string) ([]byte, error) {
		switch {
		case name == b.agentBin:
			return []byte("0.1.0\n"), nil
		case name == "systemctl" && args[0] == "is-active":
			return []byte("active\n"), nil
		case name == "journalctl":
			return []byte("Sep 07 10:00:00 node tgwp-agent[1]: connected to panel\n"), nil
		}
		return nil, nil
	}}
	o := b.options()
	o.Yes = true

	if err := RunUpgrade(context.Background(), o); err != nil {
		t.Fatalf("upgrade: %v\n%s", err, b.out.String())
	}
	if got := b.agentContent(t); got != string(newBin) {
		t.Fatalf("installed binary = %q", got)
	}
	if !b.exec.ranCmd("systemctl restart tgwp-agent") {
		t.Error("the unit was never restarted")
	}
	if _, err := os.Stat(b.agentBin + ".prev"); err == nil {
		t.Error("the kept copy must be removed once the new binary is proven")
	}
	if _, err := os.Stat(filepath.Join(b.dir, "tgwp-agent.new")); err == nil {
		t.Error("the staging file must not be left behind")
	}
}

// TestUpgradeAgentRollsBack: the new binary installs and the unit restarts, but the agent
// never reports back. The previous binary must be put back from the copy on disk - no second
// download - and the unit restarted with it.
func TestUpgradeAgentRollsBack(t *testing.T) {
	newBin := []byte("new agent binary")
	panel := newStubPanel(t, newBin, sha256Hex(newBin))
	b := newUpgradeBench(t, panel.srv.URL, []byte("old agent"))
	restarts := 0
	b.exec = &upgradeExec{fn: func(name string, args []string) ([]byte, error) {
		switch {
		case name == b.agentBin:
			return []byte("0.1.0\n"), nil
		case name == "systemctl" && args[0] == "restart":
			restarts++
			return nil, nil
		case name == "systemctl" && args[0] == "is-active":
			return []byte("active\n"), nil
		case name == "journalctl":
			return []byte("Sep 07 10:00:00 node tgwp-agent[1]: session ended\n"), nil
		}
		return nil, nil
	}}
	o := b.options()
	o.Yes = true

	err := RunUpgrade(context.Background(), o)
	if err == nil {
		t.Fatalf("expected a failure\n%s", b.out.String())
	}
	if got := b.agentContent(t); got != "old agent" {
		t.Fatalf("the previous binary was not restored: %q", got)
	}
	if restarts != 2 {
		t.Errorf("restarts = %d, want 2 (the upgrade and the rollback)", restarts)
	}
	if !strings.Contains(b.out.String(), "rolled back") {
		t.Errorf("the rollback was not reported:\n%s", b.out.String())
	}
}

// TestUpgradeCheckChangesNothing: --check reports the plan and touches neither the binary nor
// systemd, and it needs no confirmation.
func TestUpgradeCheckChangesNothing(t *testing.T) {
	newBin := []byte("new agent binary")
	panel := newStubPanel(t, newBin, sha256Hex(newBin))
	b := newUpgradeBench(t, panel.srv.URL, []byte("old agent"))
	b.exec = &upgradeExec{fn: func(name string, args []string) ([]byte, error) {
		if name == b.agentBin {
			return []byte("0.1.0\n"), nil
		}
		return nil, nil
	}}
	o := b.options()
	o.Check = true

	if err := RunUpgrade(context.Background(), o); err != nil {
		t.Fatalf("--check must not fail: %v\n%s", err, b.out.String())
	}
	if got := b.agentContent(t); got != "old agent" {
		t.Fatalf("--check replaced the binary: %q", got)
	}
	if b.exec.ranCmd("systemctl restart tgwp-agent") {
		t.Error("--check restarted a unit")
	}
	out := b.out.String()
	for _, want := range []string{"0.1.0 → 0.2.0", "nothing was changed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// The node token is in the env file this command just read; it must never be printed.
	if strings.Contains(out, "node-token") {
		t.Errorf("the node token leaked into the output:\n%s", out)
	}
}

// TestUpgradeWithoutTTYNeedsYes: unattended and unconfirmed is the one combination that must
// stop before doing anything.
func TestUpgradeWithoutTTYNeedsYes(t *testing.T) {
	newBin := []byte("new agent binary")
	panel := newStubPanel(t, newBin, sha256Hex(newBin))
	b := newUpgradeBench(t, panel.srv.URL, []byte("old agent"))
	b.exec = &upgradeExec{fn: func(name string, args []string) ([]byte, error) {
		if name == b.agentBin {
			return []byte("0.1.0\n"), nil
		}
		return nil, nil
	}}

	err := RunUpgrade(context.Background(), b.options())
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v, want a --yes hint", err)
	}
	if got := b.agentContent(t); got != "old agent" {
		t.Fatalf("binary = %q", got)
	}
}

// TestUpgradeNothingToDo: a node already at the pin is a no-op that says so.
func TestUpgradeNothingToDo(t *testing.T) {
	newBin := []byte("current agent")
	panel := newStubPanel(t, newBin, sha256Hex(newBin))
	b := newUpgradeBench(t, panel.srv.URL, newBin)
	b.exec = &upgradeExec{fn: func(name string, args []string) ([]byte, error) {
		if name == b.agentBin {
			return []byte("0.2.0\n"), nil
		}
		return nil, nil
	}}
	if err := RunUpgrade(context.Background(), b.options()); err != nil {
		t.Fatalf("err = %v\n%s", err, b.out.String())
	}
	if !strings.Contains(b.out.String(), "already at the pinned version") {
		t.Errorf("output:\n%s", b.out.String())
	}
}

// TestExtractTelemtBinary: the release tarball's layout changes between versions, so the
// binary is found by name wherever it sits.
func TestExtractTelemtBinary(t *testing.T) {
	dir := t.TempDir()
	tgz := filepath.Join(dir, "telemt.tar.gz")
	writeTarGz(t, tgz, map[string]string{
		"telemt-3.5.6/LICENSE":    "license",
		"telemt-3.5.6/bin/telemt": "ELF",
		"telemt-3.5.6/README.md":  "readme",
	})
	dst := filepath.Join(dir, "telemt")
	if err := extractTelemtBinary(tgz, dst); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dst)
	if err != nil || string(raw) != "ELF" {
		t.Fatalf("extracted %q %v", raw, err)
	}
	fi, err := os.Stat(dst)
	if err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v %v", fi.Mode(), err)
	}

	empty := filepath.Join(dir, "empty.tar.gz")
	writeTarGz(t, empty, map[string]string{"telemt-3.5.6/README.md": "readme"})
	if err := extractTelemtBinary(empty, dst); err == nil {
		t.Fatal("a tarball without a telemt binary must be an error")
	}
}

// TestBackupAndRestoreBinary pins the rollback mechanic: the copy exists while the new binary
// is on trial, and restoring it needs nothing from the network.
func TestBackupAndRestoreBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "thing")
	if err := os.WriteFile(bin, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev, err := backupBinary(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := restoreBinary(prev, bin); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(bin)
	if string(raw) != "v1" {
		t.Fatalf("restored %q", raw)
	}
	if _, err := os.Stat(prev); err == nil {
		t.Error("restoring must consume the kept copy")
	}

	// Nothing installed yet: there is no copy to keep, and rolling back means removing what
	// was just put there rather than failing.
	fresh := filepath.Join(dir, "absent")
	prev, err = backupBinary(fresh)
	if err != nil || prev != "" {
		t.Fatalf("backupBinary(missing) = %q, %v; want \"\", nil", prev, err)
	}
	if err := os.WriteFile(fresh, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := restoreBinary(prev, fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fresh); err == nil {
		t.Error("with no kept copy the rollback must remove the installed binary")
	}
}

// writeTarGz builds a gzipped tar with the given path -> content entries.
func writeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []io.Closer{tw, gz, f} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
