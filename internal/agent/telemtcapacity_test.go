package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"testing"

	"tgwebproxy/internal/domain"
	agentv1 "tgwebproxy/proto/agent/v1"
)

func writeInstalledTelemtConfig(t *testing.T, cfg Config, mode os.FileMode) []byte {
	t.Helper()
	raw, err := RenderTelemtConfig(TelemtInitParams{
		Hostname: "n1.example.com", PublicIP: "203.0.113.7", ClassicPort: 8443,
		WebUser: "node", WebSecret: secretNode,
	}, "tok-abc")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.TelemtConfigPath, raw, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cfg.TelemtConfigPath, mode); err != nil {
		t.Fatal(err)
	}
	return raw
}

func restarts(ex *fakeExec) int {
	n := 0
	for _, c := range ex.list() {
		if c == "systemctl restart telemt" {
			n++
		}
	}
	return n
}

func limitPatches(patches []string) []map[string]any {
	var out []map[string]any
	for _, p := range patches {
		var body map[string]any
		if err := json.Unmarshal([]byte(p), &body); err != nil {
			continue
		}
		web, _ := body["web"].(map[string]any)
		if limits, ok := web["limits"].(map[string]any); ok {
			if len(body) != 1 || len(web) != 1 {
				return []map[string]any{{"__unexpected__": p}}
			}
			out = append(out, limits)
		}
	}
	return out
}

func fileOwner(t *testing.T, path string) (uint32, uint32, os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	return st.Uid, st.Gid, info.Mode().Perm()
}

func capacityRequest(capacity, maxProfiles uint32) *agentv1.ApplyRequest {
	return &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode),
		WebCapacity: capacity, MaxProfiles: maxProfiles,
	}
}

func TestTomlServerMaxConnections(t *testing.T) {
	raw := []byte("[general]\nmax_connections = 5\n[server]\nport = 8443 # fake-tls\nmetrics_whitelist = [\"127.0.0.1/32\"]\n[server.api]\nenabled = true\n")
	if n, err := tomlServerMaxConnections(raw); err != nil || n != telemtDefaultMaxConnections {
		t.Fatalf("an absent key is telemt's default: %d %v", n, err)
	}
	next, err := setTomlServerMaxConnections(raw, 40000)
	if err != nil {
		t.Fatal(err)
	}
	want := "[general]\nmax_connections = 5\n[server]\nmax_connections = 40000\nport = 8443 # fake-tls\nmetrics_whitelist = [\"127.0.0.1/32\"]\n[server.api]\nenabled = true\n"
	if string(next) != want {
		t.Fatalf("insert:\n%s", next)
	}
	again, err := setTomlServerMaxConnections([]byte(strings.Replace(want, "max_connections = 40000", "max_connections = 20_000 # tuned", 1)), 10000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(again), "\nmax_connections = 10000 # tuned\n") || strings.Count(string(again), "max_connections") != 2 {
		t.Fatalf("replace:\n%s", again)
	}
	if n, _ := tomlServerMaxConnections(again); n != 10000 {
		t.Fatalf("read back %d", n)
	}
	if _, err := setTomlServerMaxConnections([]byte("[general]\n"), 1); err == nil {
		t.Fatal("a config without [server] is refused, not guessed at")
	}
}

func TestTelemtCapacityChangeRewritesLimitsAndRestartsOnce(t *testing.T) {
	ex := &fakeExec{}
	h, cfg, ft := telemtHandler(t, ex)
	ft.version = "3.5.14"
	before := writeInstalledTelemtConfig(t, cfg, 0o640)
	uid, gid, _ := fileOwner(t, cfg.TelemtConfigPath)

	res := h.Apply(context.Background(), capacityRequest(1024, 0))
	if !res.Ok || !res.RestartedRelay {
		t.Fatalf("apply: %+v\n%s", res, res.Log)
	}
	if n := restarts(ex); n != 1 {
		t.Fatalf("telemt restarted %d times", n)
	}
	_, _, patches, _ := ft.snapshot()
	sent := limitPatches(patches)
	preset, _ := domain.WebCapacityPreset(1024)
	if len(sent) != 1 || len(sent[0]) != len(preset.Limits.Fields()) {
		t.Fatalf("one sparse [web.limits] patch with the preset only, got %v", sent)
	}
	for _, f := range preset.Limits.Fields() {
		if sent[0][f.Key] != float64(f.Value) {
			t.Fatalf("%s = %v, want %d", f.Key, sent[0][f.Key], f.Value)
		}
	}
	if web := ft.section("web"); web["carrier"] != "https" || web["vhosts"] == nil {
		t.Fatalf("the rest of [web] must be left alone: %v", web)
	}

	after, err := os.ReadFile(cfg.TelemtConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := tomlServerMaxConnections(after); n != 40000 {
		t.Fatalf("server.max_connections = %d", n)
	}
	if got := strings.Replace(string(after), "[server]\nmax_connections = 40000\n", "[server]\n", 1); got != string(before) {
		t.Fatalf("every other line must stay as it was:\n%s", after)
	}
	if u, g, mode := fileOwner(t, cfg.TelemtConfigPath); u != uid || g != gid || mode != 0o640 {
		t.Fatalf("owner %d:%d mode %o, want %d:%d 640", u, g, mode, uid, gid)
	}

	ft.resetWrites()
	res = h.Apply(context.Background(), capacityRequest(1024, 0))
	if !res.Ok || res.RestartedRelay {
		t.Fatalf("a repeated apply must not restart: %+v\n%s", res, res.Log)
	}
	if n := restarts(ex); n != 1 {
		t.Fatalf("telemt restarted %d times", n)
	}
	_, _, patches, _ = ft.snapshot()
	if len(limitPatches(patches)) != 0 {
		t.Fatalf("nothing to patch the second time: %v", patches)
	}
	if again, _ := os.ReadFile(cfg.TelemtConfigPath); string(again) != string(after) {
		t.Fatal("an unchanged capacity must not rewrite telemt.toml")
	}

	rep := h.healthTelemt(context.Background())
	if rep.WebCapacity != 1024 || rep.MaxConnections != 40000 {
		t.Fatalf("reported %d sessions / %d connections", rep.WebCapacity, rep.MaxConnections)
	}
}

func TestTelemtStandardCapacityOnAnInstalledNodeIsANoop(t *testing.T) {
	ex := &fakeExec{}
	h, cfg, ft := telemtHandler(t, ex)
	before := writeInstalledTelemtConfig(t, cfg, 0o600)
	res := h.Apply(context.Background(), capacityRequest(domain.WebCapacityStandard, 0))
	if !res.Ok || res.RestartedRelay || restarts(ex) != 0 {
		t.Fatalf("the installer already runs telemt's defaults: %+v\n%s", res, res.Log)
	}
	_, _, patches, _ := ft.snapshot()
	if len(limitPatches(patches)) != 0 {
		t.Fatalf("patched %v", patches)
	}
	if after, _ := os.ReadFile(cfg.TelemtConfigPath); string(after) != string(before) {
		t.Fatal("telemt.toml rewritten for nothing")
	}
	rep := h.healthTelemt(context.Background())
	if rep.WebCapacity != domain.WebCapacityStandard || rep.MaxConnections != telemtDefaultMaxConnections {
		t.Fatalf("reported %d / %d", rep.WebCapacity, rep.MaxConnections)
	}
}

func TestTelemtMaxProfilesFollowsTheNodeKeyCap(t *testing.T) {
	ex := &fakeExec{}
	h, cfg, ft := telemtHandler(t, ex)
	writeInstalledTelemtConfig(t, cfg, 0o600)
	res := h.Apply(context.Background(), capacityRequest(0, 128))
	if !res.Ok || !res.RestartedRelay || restarts(ex) != 1 {
		t.Fatalf("telemt's 32 profiles must be raised for 128 keys: %+v\n%s", res, res.Log)
	}
	_, _, patches, _ := ft.snapshot()
	sent := limitPatches(patches)
	if len(sent) != 1 || len(sent[0]) != 1 || sent[0]["max_profiles"] != float64(domain.WebMaxProfiles(128)) {
		t.Fatalf("only max_profiles is raised, got %v", sent)
	}

	ft.resetWrites()
	res = h.Apply(context.Background(), capacityRequest(0, 64))
	if !res.Ok || res.RestartedRelay || restarts(ex) != 1 {
		t.Fatalf("a lower key cap never shrinks max_profiles: %+v\n%s", res, res.Log)
	}
	_, _, patches, _ = ft.snapshot()
	if len(limitPatches(patches)) != 0 {
		t.Fatalf("patched %v", patches)
	}
}

func TestTelemtCapacityRollsBackWhenTelemtDoesNotComeUp(t *testing.T) {
	ex := &fakeExec{failNth: "systemctl restart telemt", failNthCount: 1}
	h, cfg, ft := telemtHandler(t, ex)
	ft.version = "3.5.14"
	before := writeInstalledTelemtConfig(t, cfg, 0o640)

	res := h.Apply(context.Background(), capacityRequest(512, 0))
	if res.Ok || !res.RolledBack {
		t.Fatalf("a failed restart must roll back: %+v\n%s", res, res.Log)
	}
	if after, _ := os.ReadFile(cfg.TelemtConfigPath); string(after) != string(before) {
		t.Fatalf("telemt.toml must be the one telemt last started with:\n%s", after)
	}
	if _, _, mode := fileOwner(t, cfg.TelemtConfigPath); mode != 0o640 {
		t.Fatalf("restored file mode %o", mode)
	}
	if n := restarts(ex); n != 2 {
		t.Fatalf("one failed restart and one with the previous config, got %d", n)
	}
	if !strings.Contains(res.Log, "rolling back") {
		t.Fatal(res.Log)
	}
}

func TestTelemtCapacityNeedsTelemt3514(t *testing.T) {
	ex := &fakeExec{}
	h, cfg, ft := telemtHandler(t, ex)
	before := writeInstalledTelemtConfig(t, cfg, 0o600)
	res := h.Apply(context.Background(), capacityRequest(256, 0))
	if res.Ok || !strings.Contains(res.Log, "Telemt 3.5.14") {
		t.Fatalf("an older telemt is refused: %+v\n%s", res, res.Log)
	}
	_, _, patches, _ := ft.snapshot()
	if len(limitPatches(patches)) != 0 || restarts(ex) != 0 {
		t.Fatalf("nothing may change: %v", patches)
	}
	if after, _ := os.ReadFile(cfg.TelemtConfigPath); string(after) != string(before) {
		t.Fatal("telemt.toml changed")
	}
}

func TestTelemtCapacityRefusesAnUnknownPreset(t *testing.T) {
	h, cfg, _ := telemtHandler(t, &fakeExec{})
	writeInstalledTelemtConfig(t, cfg, 0o600)
	if res := h.Apply(context.Background(), capacityRequest(300, 0)); res.Ok {
		t.Fatal("300 is not a preset")
	}
}
