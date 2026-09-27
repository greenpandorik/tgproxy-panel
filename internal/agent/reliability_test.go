package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tgwebproxy/internal/reliability"
)

func TestConnectionSamplerResetsAndUsesWindow(t *testing.T) {
	var s connectionSampler
	now := time.Now()
	if s.sample(now, 1000, 100) != nil {
		t.Fatal("lifetime used")
	}
	w := s.sample(now.Add(30*time.Second), 1001, 110)
	if w == nil || w.Success != 1 || w.Failed != 10 {
		t.Fatalf("window %+v", w)
	}
	if s.sample(now.Add(time.Minute), 0, 0) != nil {
		t.Fatal("restart not reset")
	}
}

func TestAssetsOnlyResources(t *testing.T) {
	paths, e := localAssets([]byte(`<link rel="canonical" href="/"><a href="/contact">Contact</a><link rel="stylesheet" href="/app.css"><script src="app.js?v=1"></script><img src="https://cdn.example/a.png">`))
	if e != nil || len(paths) != 2 || paths[0] != "app.css" || paths[1] != "app.js" {
		t.Fatalf("%v %v", paths, e)
	}
	if _, e = localAssets([]byte(`<script src="../secret"></script>`)); e == nil {
		t.Fatal("traversal accepted")
	}
}

func TestScopeArrayPreserved(t *testing.T) {
	for _, v := range []any{[]any{"mask"}, "mask", []string{"web"}} {
		if !hasScope(v) {
			t.Fatal("scope lost")
		}
	}
	if hasScope([]any{}) || hasScope(nil) {
		t.Fatal("empty scope preserved")
	}
}

func TestJournalBlocksOverwriteAndChecksArtifacts(t *testing.T) {
	h := &Handler{cfg: Config{StateDir: t.TempDir()}}
	if e := h.checkUpdateJournal(); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(updateJournal{Active: true})
	if e := os.WriteFile(h.updateJournalPath(), raw, 0o600); e != nil {
		t.Fatal(e)
	}
	if h.checkUpdateJournal() == nil {
		t.Fatal("active journal ignored")
	}
	src := filepath.Join(t.TempDir(), "source")
	dst := filepath.Join(t.TempDir(), "copy")
	if err := os.WriteFile(src, []byte("known good"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum, e := saveVerifiedCopy(src, dst, 0o600)
	if e != nil {
		t.Fatal(e)
	}
	if err := os.WriteFile(dst, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, e = verifiedFile(dst, sum); e == nil {
		t.Fatal("corrupt rollback accepted")
	}
}

func TestMaintenanceChangeDoesNotRestorePrimary(t *testing.T) {
	policy := reliability.DefaultPolicy()
	policy.Egress = "direct"
	policy.ReserveSOCKSAddress = "127.0.0.1:1081"
	policy.AutomaticFailover = true
	h := &Handler{cfg: Config{StateDir: t.TempDir(), Engine: EngineTelemt}, recoveryPolicy: policy, activeEgress: policy.ReserveSOCKSAddress}
	policy.Maintenance = true
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.configureReliability(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	var state recoveryState
	if err = json.Unmarshal(result, &state); err != nil {
		t.Fatal(err)
	}
	if !state.Policy.Maintenance || state.Active != "127.0.0.1:1081" {
		t.Fatalf("maintenance unexpectedly changed route: %+v", state)
	}
}
