package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	agentv1 "tgwebproxy/proto/agent/v1"
)

type nftExec struct {
	mu       sync.Mutex
	present  bool
	missing  bool
	refuse   string
	applied  []string
	listings int
}

func (f *nftExec) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if name != "nft" {
		return nil, nil
	}
	if f.missing {
		return nil, &exec.Error{Name: "nft", Err: exec.ErrNotFound}
	}
	if args[0] == "-f" {
		raw, _ := os.ReadFile(args[1])
		if f.refuse != "" {
			return []byte(f.refuse), errors.New("exit status 1")
		}
		f.applied = append(f.applied, string(raw))
		f.present = strings.Contains(string(raw), "table inet tgwp_block {")
		return nil, nil
	}
	f.listings++
	if !f.present {
		return []byte("Error: No such file or directory"), errors.New("exit status 1")
	}
	return os.ReadFile("testdata/nft-list.json")
}

func (f *nftExec) Start(context.Context, string, ...string) (io.ReadCloser, error) { return nil, nil }

func firewallHandler(t *testing.T, ex Exec) *Handler {
	t.Helper()
	return NewHandler(Config{StateDir: t.TempDir()}, ex, slog.New(slog.DiscardHandler))
}

func TestRenderFirewallSplitsFamiliesAndKeepsReplies(t *testing.T) {
	got := renderFirewall([]string{"203.0.113.7", "198.51.100.0/24", "2001:db8::/32"})
	for _, want := range []string{
		"table inet tgwp_block\ndelete table inet tgwp_block\n",
		"elements = { 203.0.113.7/32, 198.51.100.0/24 }",
		"elements = { 2001:db8::/32 }",
		"ct direction reply accept\n\t\tip saddr @v4 counter drop",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}
	if empty := renderFirewall(nil); strings.Contains(empty, "{") {
		t.Fatalf("an empty list should only remove the table:\n%s", empty)
	}
}

func TestParseNftCountersReadsTotalsAndEachEntry(t *testing.T) {
	raw, err := os.ReadFile("testdata/nft-list.json")
	if err != nil {
		t.Fatal(err)
	}
	total, per, err := parseNftCounters(raw)
	if err != nil {
		t.Fatal(err)
	}
	if total.Packets != 3 || total.Bytes != 252 {
		t.Fatalf("total %+v", total)
	}
	if per["198.51.100.0/24"].Packets != 3 || per["203.0.113.7"].Packets != 0 || len(per) != 3 {
		t.Fatalf("per entry %+v", per)
	}
}

func TestFirewallSetSurvivesARestartAndComesBackWhenRemoved(t *testing.T) {
	ex := &nftExec{}
	h := firewallHandler(t, ex)
	st, err := h.configureFirewall(context.Background(), &agentv1.FirewallRequest{Set: true, Revision: 4, Entries: []string{"198.51.100.0/24", "203.0.113.7", "2001:db8::/32"}})
	if err != nil {
		t.Fatal(err)
	}
	if st.Revision != 4 || st.Entries != 3 || st.DroppedPackets != 3 || len(st.Counters) != 3 || st.Counters[0].Packets != 3 {
		t.Fatalf("status %+v", st)
	}

	again := NewHandler(Config{StateDir: h.cfg.StateDir}, &nftExec{}, slog.New(slog.DiscardHandler))
	if err := again.restoreFirewall(context.Background()); err != nil {
		t.Fatal(err)
	}
	fresh := again.exec.(*nftExec)
	if len(fresh.applied) != 1 || !strings.Contains(fresh.applied[0], "198.51.100.0/24") {
		t.Fatalf("restart did not put the rules back: %v", fresh.applied)
	}

	fresh.present = false
	if hb := again.firewallStatus(context.Background()); hb.Revision != 4 || hb.Error != "" || hb.DroppedPackets != 3 || hb.Counters != nil {
		t.Fatalf("heartbeat %+v", hb)
	}
	if len(fresh.applied) != 2 {
		t.Fatal("a removed table should be put back")
	}
}

func TestFirewallReportsWhatGoesWrong(t *testing.T) {
	ex := &nftExec{missing: true}
	h := firewallHandler(t, ex)
	if _, err := h.configureFirewall(context.Background(), &agentv1.FirewallRequest{Set: true, Revision: 1}); err != nil {
		t.Fatalf("clearing an empty list needs no nftables: %v", err)
	}
	_, err := h.configureFirewall(context.Background(), &agentv1.FirewallRequest{Set: true, Revision: 2, Entries: []string{"203.0.113.7"}})
	if err == nil || !strings.Contains(err.Error(), "apt-get install nftables") {
		t.Fatalf("missing nft: %v", err)
	}
	if st := h.firewallStatus(context.Background()); st.Revision != 1 || !strings.Contains(st.Error, "nftables") {
		t.Fatalf("the heartbeat should carry the failure and the revision still in force: %+v", st)
	}

	ex.missing, ex.refuse = false, "Error: Could not process rule"
	if _, err := h.configureFirewall(context.Background(), &agentv1.FirewallRequest{Set: true, Revision: 3, Entries: []string{"203.0.113.7"}}); err == nil || !strings.Contains(err.Error(), "Could not process rule") {
		t.Fatalf("refused rules: %v", err)
	}
	if _, err := h.configureFirewall(context.Background(), &agentv1.FirewallRequest{Set: true, Revision: 4, Entries: []string{"0.0.0.0/0"}}); err == nil {
		t.Fatal("the agent must check entries itself")
	}
}

func TestFirewallErrorClearsOnceTheRulesAreBack(t *testing.T) {
	ex := &nftExec{}
	h := firewallHandler(t, ex)
	if _, err := h.configureFirewall(context.Background(), &agentv1.FirewallRequest{Set: true, Revision: 2, Entries: []string{"203.0.113.7"}}); err != nil {
		t.Fatal(err)
	}

	again := NewHandler(Config{StateDir: h.cfg.StateDir}, &nftExec{refuse: "Error: busy"}, slog.New(slog.DiscardHandler))
	if err := again.restoreFirewall(context.Background()); err == nil {
		t.Fatal("restore should fail while nft refuses")
	}
	fresh := again.exec.(*nftExec)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	listings := fresh.listings
	if st := again.firewallStatus(cancelled); st.Error == "" || fresh.listings != listings {
		t.Fatalf("a cancelled heartbeat must not touch nft: %+v", st)
	}

	fresh.refuse = ""
	if st := again.firewallStatus(context.Background()); st.Error != "" || st.Revision != 2 {
		t.Fatalf("once the table is back the error must go: %+v", st)
	}
}
