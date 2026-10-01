package api_test

import (
	"context"
	"log/slog"
	"reflect"
	"testing"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/worker"
)

type blocklistResp struct {
	Entries []struct {
		Prefix  string  `json:"prefix"`
		Note    string  `json:"note"`
		Packets *uint64 `json:"packets"`
	} `json:"entries"`
	Revision       int64   `json:"revision"`
	Supported      *bool   `json:"supported"`
	Live           bool    `json:"live"`
	Synced         bool    `json:"synced"`
	DroppedPackets *uint64 `json:"dropped_packets"`
	ApplyError     string  `json:"apply_error"`
}

type entryIn struct {
	Prefix string `json:"prefix"`
	Note   string `json:"note,omitempty"`
}

func TestBlocklistReachesTheServerAndCountsDrops(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	h.Mock.SetOnline(n.ID, true)
	path := "/api/v1/nodes/" + n.ID.String() + "/blocklist"

	var out blocklistResp
	c.JSON(c.Get(path), &out)
	if len(out.Entries) != 0 || !out.Synced || out.Supported == nil || !*out.Supported {
		t.Fatalf("empty list %+v", out)
	}

	resp := c.Put(path, map[string]any{"entries": []entryIn{{Prefix: "203.0.113.9"}, {Prefix: "0.0.0.0/0"}, {Prefix: "203.0.113.0/24"}}})
	var bad struct {
		Error struct {
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	c.JSON(resp, &bad)
	if resp.StatusCode != 422 || bad.Error.Fields["entries.1"] != "too wide" || bad.Error.Fields["entries.2"] != "covers 203.0.113.9 on line 1" {
		t.Fatalf("bad list %d %v", resp.StatusCode, bad.Error.Fields)
	}

	resp = c.Put(path, map[string]any{"entries": []entryIn{{Prefix: "198.51.100.7/24", Note: "scanner"}, {Prefix: "203.0.113.9"}}})
	c.JSON(resp, &out)
	if resp.StatusCode != 200 || out.Revision != 1 || !out.Live || !out.Synced || out.Entries[0].Prefix != "198.51.100.0/24" || out.Entries[0].Note != "scanner" {
		t.Fatalf("saved %d %+v", resp.StatusCode, out)
	}
	if rev, got := h.Mock.FirewallEntries(n.ID); rev != 1 || !reflect.DeepEqual(got, []string{"198.51.100.0/24", "203.0.113.9"}) {
		t.Fatalf("server got %d %v", rev, got)
	}

	h.Mock.SetFirewallDrops(n.ID, "198.51.100.0/24", 42)
	c.JSON(c.Get(path), &out)
	if out.Entries[0].Packets == nil || *out.Entries[0].Packets != 42 || *out.DroppedPackets != 42 {
		t.Fatalf("drops %+v", out)
	}

	h.Mock.FailFirewall(n.ID, "nftables is not installed on the server: apt-get install nftables")
	resp = c.Put(path, map[string]any{"entries": []entryIn{{Prefix: "203.0.113.9"}}})
	c.JSON(resp, &out)
	if resp.StatusCode != 200 || out.Revision != 2 || out.Synced || out.ApplyError == "" {
		t.Fatalf("a refused list is still saved and the reason shown: %d %+v", resp.StatusCode, out)
	}
}

func TestBlocklistCatchesUpWhenTheServerComesBack(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	path := "/api/v1/nodes/" + n.ID.String() + "/blocklist"

	var out blocklistResp
	resp := c.Put(path, map[string]any{"entries": []entryIn{{Prefix: "2001:db8::/32", Note: "bots"}}})
	c.JSON(resp, &out)
	if resp.StatusCode != 200 || out.Revision != 1 || out.Synced || out.ApplyError != "" {
		t.Fatalf("offline save %d %+v", resp.StatusCode, out)
	}

	h.Mock.SetOnline(n.ID, true)
	h.Presence.OnHeartbeat(context.Background(), n.ID, nodedriver.HealthToProto(nodedriver.HealthReport{
		RelayActive: true, MTProxyActive: true, Healthz: true, Readyz: true, Firewall: &nodedriver.FirewallStatus{},
	}))
	if err := worker.NewBlocklists(h.Store, h.Mock, slog.New(slog.DiscardHandler)).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rev, got := h.Mock.FirewallEntries(n.ID); rev != 1 || !reflect.DeepEqual(got, []string{"2001:db8::/32"}) {
		t.Fatalf("the server should get the list once it is back: %d %v", rev, got)
	}
}

func TestBlocklistIsUnknownForAServerThatNeverReported(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	h.Presence.OnHeartbeat(context.Background(), n.ID, nodedriver.HealthToProto(nodedriver.HealthReport{RelayActive: true}))
	var out blocklistResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/blocklist"), &out)
	if out.Supported != nil {
		t.Fatalf("an offline server that never reported a blocklist is unknown, not unsupported: %+v", out)
	}
}

func TestBlocklistClearsRulesLeftFromAnEarlierInstall(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	h.Mock.SetOnline(n.ID, true)
	if _, err := h.Mock.Firewall(context.Background(), n.ID, true, 5, []string{"203.0.113.7"}); err != nil {
		t.Fatal(err)
	}
	h.Presence.OnHeartbeat(context.Background(), n.ID, nodedriver.HealthToProto(nodedriver.HealthReport{
		RelayActive: true, MTProxyActive: true, Healthz: true, Readyz: true, Firewall: &nodedriver.FirewallStatus{Revision: 5, Entries: 1},
	}))
	h.Mock.SetOnline(n.ID, false)

	var out struct {
		Synced      bool `json:"synced"`
		NodeEntries *int `json:"node_entries"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/blocklist"), &out)
	if out.Synced || out.NodeEntries == nil || *out.NodeEntries != 1 {
		t.Fatalf("rules the panel does not know about must show as out of sync: %+v", out)
	}

	h.Mock.SetOnline(n.ID, true)
	if err := worker.NewBlocklists(h.Store, h.Mock, slog.New(slog.DiscardHandler)).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rev, got := h.Mock.FirewallEntries(n.ID); rev != 0 || len(got) != 0 {
		t.Fatalf("old rules should be cleared: %d %v", rev, got)
	}
}

func TestBlocklistRefusesToOverwriteANewerList(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	path := "/api/v1/nodes/" + n.ID.String() + "/blocklist"

	if resp := c.Put(path, map[string]any{"revision": 0, "entries": []entryIn{{Prefix: "203.0.113.7"}}}); resp.StatusCode != 200 {
		t.Fatalf("first save %d", resp.StatusCode)
	}
	if resp := c.Put(path, map[string]any{"revision": 0, "entries": []entryIn{{Prefix: "198.51.100.0/24"}}}); resp.StatusCode != 409 {
		t.Fatalf("a save based on an older list must be refused, got %d", resp.StatusCode)
	}
	if resp := c.Put(path, map[string]any{"revision": 1, "entries": []entryIn{{Prefix: "203.0.113.7"}, {Prefix: "198.51.100.0/24"}}}); resp.StatusCode != 200 {
		t.Fatalf("a save based on the current list %d", resp.StatusCode)
	}
}
