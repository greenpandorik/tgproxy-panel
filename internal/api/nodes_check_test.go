package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"tgwebproxy/internal/api"
	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/nodecheck"
)

type checkResultResp struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail"`
	Advisory bool   `json:"advisory"`
}

type checkReportResp struct {
	RanAt   time.Time         `json:"ran_at"`
	Results []checkResultResp `json:"results"`
	AllOK   bool              `json:"all_ok"`
}

// noopResolver never finds a record, and noopDial never connects - together
// they make every probe fail deterministically, without the test touching
// the real network (the test node's hostname, "n1.test", isn't a real
// domain anyway, but a live DNS lookup for it is slow and environment
// dependent).
type noopResolver struct{}

func (noopResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return nil, errors.New("no such host")
}

func noopDial(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("dial disabled in this test")
}

// ownerWithNodeAndChecker is like ownerWithNode but wires a deterministic,
// network-free Checker into the server so /nodes/{id}/check is reproducible
// in CI regardless of the sandbox's DNS/network access.
func ownerWithNodeAndChecker(t *testing.T) (*apitest.Harness, *apitest.Client, nodeResp) {
	t.Helper()
	checker := &nodecheck.Checker{Resolver: noopResolver{}, Dial: noopDial, Timeout: 2 * time.Second}
	h := apitest.New(t, func(d *api.Deps) { d.NodeChecker = checker })
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	return h, c, n
}

func TestOwnerRunsNodeCheckAndItPersists(t *testing.T) {
	_, c, n := ownerWithNodeAndChecker(t)

	var report checkReportResp
	resp := c.Post("/api/v1/nodes/"+n.ID.String()+"/check", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check expected 200, got %d", resp.StatusCode)
	}
	c.JSON(resp, &report)
	// Seven probes on a telemt node (the default engine): the six public prerequisites
	// (including the advisory pq_kex) plus the telemt-only `mask` probe of the Fake-TLS
	// listener.
	if len(report.Results) != 7 {
		t.Fatalf("expected 7 results, got %+v", report.Results)
	}
	if report.Results[6].Name != "mask" {
		t.Fatalf("last probe must be the mask check, got %+v", report.Results)
	}
	if report.AllOK {
		t.Fatalf("expected AllOK=false with every probe stubbed to fail, got %+v", report.Results)
	}
	// The advisory flag travels through the API and into the persisted report.
	if pq := report.Results[4]; pq.Name != "pq_kex" || !pq.Advisory {
		t.Fatalf("pq_kex must follow tls_cert and be advisory, got %+v", report.Results)
	}

	var got struct {
		LastCheck *checkReportResp `json:"last_check"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &got)
	if got.LastCheck == nil {
		t.Fatalf("expected last_check to be persisted on the node")
	}
	if len(got.LastCheck.Results) != len(report.Results) {
		t.Fatalf("persisted report mismatch: %+v", got.LastCheck)
	}

	var audit struct {
		Items []struct {
			Action string          `json:"action"`
			Meta   json.RawMessage `json:"meta"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/audit"), &audit)
	found := false
	for _, item := range audit.Items {
		if item.Action == "node.check" {
			found = true
			var meta struct {
				AllOK bool `json:"all_ok"`
			}
			_ = json.Unmarshal(item.Meta, &meta)
			if meta.AllOK != report.AllOK {
				t.Fatalf("audit meta all_ok=%v, report all_ok=%v", meta.AllOK, report.AllOK)
			}
		}
	}
	if !found {
		t.Fatalf("expected a node.check audit entry, got %+v", audit.Items)
	}
}

// TestViewerSeesNoCheckDetailText covers Task 33 item 1: a viewer reading a
// node's last_check gets name/ok/ran_at/all_ok like anyone else, but the
// per-probe detail strings (which can leak resolved IPs, TLS errors, private
// ranges etc.) are only visible to writers (owner/admin).
func TestViewerSeesNoCheckDetailText(t *testing.T) {
	h, c, n := ownerWithNodeAndChecker(t)
	if resp := c.Post("/api/v1/nodes/"+n.ID.String()+"/check", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner check expected 200, got %d", resp.StatusCode)
	}

	h.CreateAdmin("v", "pass-123456", "viewer")
	viewer := h.Login("v", "pass-123456")

	var got struct {
		LastCheck *checkReportResp `json:"last_check"`
	}
	viewer.JSON(viewer.Get("/api/v1/nodes/"+n.ID.String()), &got)
	if got.LastCheck == nil {
		t.Fatalf("expected last_check to be visible to a viewer")
	}
	if len(got.LastCheck.Results) == 0 {
		t.Fatalf("expected results to be visible to a viewer")
	}
	if !got.LastCheck.AllOK && got.LastCheck.RanAt.IsZero() {
		t.Fatalf("expected ran_at to be visible to a viewer, got %+v", got.LastCheck)
	}
	for _, res := range got.LastCheck.Results {
		if res.Name == "" {
			t.Fatalf("expected name to be visible to a viewer, got %+v", res)
		}
		if res.Detail != "" {
			t.Fatalf("expected empty detail for a viewer, got %q", res.Detail)
		}
	}

	// The owner (writer) still sees the detail text.
	var ownerGot struct {
		LastCheck *checkReportResp `json:"last_check"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &ownerGot)
	sawDetail := false
	for _, res := range ownerGot.LastCheck.Results {
		if res.Detail != "" {
			sawDetail = true
		}
	}
	if !sawDetail {
		t.Fatalf("expected the owner to still see at least one non-empty detail, got %+v", ownerGot.LastCheck.Results)
	}
}

func TestNodeGetShowsNilLastCheckBeforeAnyRun(t *testing.T) {
	_, c, n := ownerWithNode(t)
	var got struct {
		LastCheck *checkReportResp `json:"last_check"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &got)
	if got.LastCheck != nil {
		t.Fatalf("expected nil last_check before any check has run, got %+v", got.LastCheck)
	}
}

func TestViewerCannotRunNodeCheck(t *testing.T) {
	h, _, n := ownerWithNodeAndChecker(t)
	h.CreateAdmin("v", "pass-123456", "viewer")
	viewer := h.Login("v", "pass-123456")
	if resp := viewer.Post("/api/v1/nodes/"+n.ID.String()+"/check", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// M9: `unknown_sni_action = "mask"` sends unauthenticated Fake-TLS traffic to tls_domain:443,
// which is the node's own public address. The mask probe is the only check that observes the
// node reaching itself, so it runs on telemt nodes - and only there: a tproxy node has no such
// listener and its report keeps the six public probes.
func TestNodeCheckMaskProbeIsTelemtOnly(t *testing.T) {
	checker := &nodecheck.Checker{Resolver: noopResolver{}, Dial: noopDial, Timeout: 2 * time.Second}
	h := apitest.New(t, func(d *api.Deps) { d.NodeChecker = checker })
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	var out struct {
		Node nodeResp `json:"node"`
	}
	resp := c.Post("/api/v1/nodes", map[string]string{"name": "tp", "hostname": "tp.test", "acme_email": "a@b.co", "engine": "tproxy"})
	if resp.StatusCode != 201 {
		t.Fatalf("create tproxy node %d", resp.StatusCode)
	}
	c.JSON(resp, &out)

	var report checkReportResp
	c.JSON(c.Post("/api/v1/nodes/"+out.Node.ID.String()+"/check", nil), &report)
	if len(report.Results) != 6 {
		t.Fatalf("a tproxy node must keep the six public probes, got %+v", report.Results)
	}
	for _, r := range report.Results {
		if r.Name == "mask" {
			t.Fatalf("a tproxy node has no Fake-TLS listener to mask: %+v", report.Results)
		}
	}
}
