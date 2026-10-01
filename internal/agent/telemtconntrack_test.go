package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestConntrackFixReachesNodesInstalledBeforeIt(t *testing.T) {
	dir := t.TempDir()
	unit := filepath.Join(dir, "telemt.service")
	if err := os.WriteFile(unit, []byte("[Service]\nExecStart=/usr/local/bin/telemt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := &fakeExec{}
	h := NewHandler(Config{Engine: EngineTelemt, StateDir: dir}, ex, slog.New(slog.DiscardHandler))
	if err := h.ensureTelemtConntrackFix(context.Background(), unit); err != nil {
		t.Fatal(err)
	}
	want := []string{"systemctl daemon-reload", "iptables -w -t raw -N TELEMT_NOTRACK", "ip6tables -w -t raw -N TELEMT_NOTRACK"}
	if !slices.Equal(ex.calls, want) {
		t.Fatalf("calls %v", ex.calls)
	}
	if b, err := os.ReadFile(telemtConntrackDropinPath(unit)); err != nil || string(b) != telemtConntrackDropin {
		t.Fatalf("drop-in %q %v", b, err)
	}

	ex.calls = nil
	if err := h.ensureTelemtConntrackFix(context.Background(), unit); err != nil || len(ex.calls) != 0 {
		t.Fatalf("a node that already has it must be left alone: %v %v", ex.calls, err)
	}

	tproxy := NewHandler(Config{Engine: EngineTProxy, StateDir: dir}, ex, slog.New(slog.DiscardHandler))
	if err := tproxy.ensureTelemtConntrackFix(context.Background(), filepath.Join(dir, "missing.service")); err != nil || len(ex.calls) != 0 {
		t.Fatal("tproxy nodes have no telemt to fix")
	}
}
