package api_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"tgwebproxy/internal/api"
	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/nodecheck"
	"tgwebproxy/internal/nodedriver"
)

type diagCheckResp struct {
	Key    string  `json:"key"`
	Status string  `json:"status"`
	Value  *string `json:"value"`
	Detail *string `json:"detail"`
}

type diagGroupResp struct {
	Key    string          `json:"key"`
	Checks []diagCheckResp `json:"checks"`
}

type diagRunResp struct {
	ID      int64           `json:"id"`
	NodeID  string          `json:"node_id"`
	Status  string          `json:"overall_status"`
	Trigger string          `json:"trigger"`
	Groups  []diagGroupResp `json:"groups"`
	Passed  int             `json:"passed"`
	Total   int             `json:"total"`
	NotRun  int             `json:"not_run"`
}

type diagListResp struct {
	Items []diagRunResp `json:"items"`
	Total int           `json:"total"`
}

type staticResolver struct{ ip string }

func (r staticResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP(r.ip)}}, nil
}

func diagHarness(t *testing.T, dial func(context.Context, string, string) (net.Conn, error)) (*apitest.Harness, *apitest.Client, nodeResp) {
	t.Helper()
	checker := &nodecheck.Checker{Resolver: staticResolver{ip: "5.6.7.8"}, Dial: dial, Timeout: 2 * time.Second}
	h := apitest.New(t, func(d *api.Deps) { d.NodeChecker = checker })
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	return h, c, n
}

func groupOf(t *testing.T, run diagRunResp, key string) diagGroupResp {
	t.Helper()
	for _, g := range run.Groups {
		if g.Key == key {
			return g
		}
	}
	t.Fatalf("run has no group %q", key)
	return diagGroupResp{}
}

func diagStatusOf(t *testing.T, run diagRunResp, group, key string) string {
	t.Helper()
	for _, c := range groupOf(t, run, group).Checks {
		if c.Key == key {
			return c.Status
		}
	}
	t.Fatalf("group %q has no check %q", group, key)
	return ""
}

func TestNodeDiagnosticsRunsAndPersists(t *testing.T) {
	h, c, n := diagHarness(t, noopDial)
	h.Mock.SetOnline(n.ID, true)
	h.Mock.SetHealth(n.ID, nodedriver.HealthReport{
		RelayActive: true, CaddyActive: true, Healthz: true, Readyz: true,
		TProxyVersion: "telemt 3.5.7", UpstreamHealthy: true, DcDataAvailable: true,
		EffectiveLatencyMs: 30, ConnectSuccessTotal: 5,
		DCs: []nodedriver.DcLatency{{DC: 1, LatencyMs: 30, Known: true}},
	})

	var run diagRunResp
	resp := c.Post("/api/v1/nodes/"+n.ID.String()+"/diagnostics/web", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("diagnostics expected 200, got %d", resp.StatusCode)
	}
	c.JSON(resp, &run)

	if run.ID == 0 || run.NodeID != n.ID.String() || run.Trigger != "manual" {
		t.Fatalf("run = %+v", run)
	}
	if len(run.Groups) != 6 {
		t.Fatalf("expected six groups, got %d", len(run.Groups))
	}
	if run.Passed+run.NotRun == 0 || run.Passed > run.Total {
		t.Fatalf("counts passed=%d total=%d not_run=%d", run.Passed, run.Total, run.NotRun)
	}
	// The node answers the panel, so its own readings are real results, not gaps.
	if got := diagStatusOf(t, run, "telemt", "control_api"); got != "ok" {
		t.Fatalf("telemt/control_api = %q, want ok", got)
	}
	// Nothing on 443 answers here, so the checks behind it were not performed.
	if got := diagStatusOf(t, run, "tcp_tls", "tls_handshake"); got != "not_available" {
		t.Fatalf("tcp_tls/tls_handshake = %q, want not_available", got)
	}

	var list diagListResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/diagnostics"), &list)
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("history = %+v", list)
	}
	stored := list.Items[0]
	if stored.ID != run.ID || stored.Status != run.Status || stored.Trigger != "manual" {
		t.Fatalf("stored %+v does not match the run %+v", stored, run)
	}
	if stored.Passed != run.Passed || stored.Total != run.Total || stored.NotRun != run.NotRun {
		t.Fatalf("stored counts %+v differ from the run's %+v", stored, run)
	}
	if len(stored.Groups) != 6 {
		t.Fatalf("stored run lost its groups: %+v", stored.Groups)
	}
}

func TestNodeDiagnosticsHistoryIsNewestFirst(t *testing.T) {
	h, c, n := diagHarness(t, noopDial)
	h.Mock.SetOnline(n.ID, true)
	path := "/api/v1/nodes/" + n.ID.String() + "/diagnostics"
	var first, second diagRunResp
	c.JSON(c.Post(path+"/web", nil), &first)
	c.JSON(c.Post(path+"/web", nil), &second)

	var list diagListResp
	c.JSON(c.Get(path+"?limit=1"), &list)
	if len(list.Items) != 1 || list.Items[0].ID != second.ID {
		t.Fatalf("limit=1 must return the newest run, got %+v", list.Items)
	}
	c.JSON(c.Get(path), &list)
	if list.Total != 2 || list.Items[0].ID != second.ID || list.Items[1].ID != first.ID {
		t.Fatalf("history should be newest first, got %+v", list.Items)
	}
	if resp := c.Get(path + "?limit=0"); resp.StatusCode != 422 {
		t.Fatalf("limit=0 expected 422, got %d", resp.StatusCode)
	}
}

// A node the panel cannot reach at all is offline, not broken: the failures are the two
// reachability checks and everything downstream is a check that could not be performed.
func TestNodeDiagnosticsOfflineNode(t *testing.T) {
	_, c, n := diagHarness(t, noopDial)

	var run diagRunResp
	c.JSON(c.Post("/api/v1/nodes/"+n.ID.String()+"/diagnostics/web", nil), &run)
	if run.Status != "offline" {
		t.Fatalf("overall_status = %q, want offline", run.Status)
	}
	if run.NotRun <= run.Total {
		t.Fatalf("an unreachable node should be mostly not_available: %+v", run)
	}
	for _, key := range []string{"service", "control_api", "readiness"} {
		if got := diagStatusOf(t, run, "telemt", key); got != "not_available" {
			t.Fatalf("telemt/%s = %q, want not_available", key, got)
		}
	}
	if got := diagStatusOf(t, run, "telemt", "agent_link"); got != "fail" {
		t.Fatalf("telemt/agent_link = %q, want fail", got)
	}
}

func TestNodeDiagnosticsRBAC(t *testing.T) {
	h, _, n := diagHarness(t, noopDial)
	h.CreateAdmin("watcher", "pass-123456", "viewer")
	h.CreateAdmin("second", "pass-123456", "admin")
	path := "/api/v1/nodes/" + n.ID.String() + "/diagnostics"

	viewer := h.Login("watcher", "pass-123456")
	if resp := viewer.Post(path+"/web", nil); resp.StatusCode != 403 {
		t.Fatalf("viewer running diagnostics expected 403, got %d", resp.StatusCode)
	}
	if resp := viewer.Get(path); resp.StatusCode != 200 {
		t.Fatalf("viewer reading history expected 200, got %d", resp.StatusCode)
	}
	admin := h.Login("second", "pass-123456")
	if resp := admin.Post(path+"/web", nil); resp.StatusCode != 200 {
		t.Fatalf("admin running diagnostics expected 200, got %d", resp.StatusCode)
	}
	anon := h.Anonymous()
	if resp := anon.Post(path+"/web", nil); resp.StatusCode != 401 {
		t.Fatalf("anonymous expected 401, got %d", resp.StatusCode)
	}
}

// One node runs one pass at a time: diagnostics hold sockets open and a queue of them would
// pile network load onto the node the operator is already worried about.
func TestNodeDiagnosticsRejectsASecondRunOnTheSameNode(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	blocking := func(ctx context.Context, _, _ string) (net.Conn, error) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, errors.New("dial disabled in this test")
	}
	_, c, n := diagHarness(t, blocking)
	path := "/api/v1/nodes/" + n.ID.String() + "/diagnostics/web"

	statuses := make(chan int, 1)
	go func() { statuses <- c.Do(http.MethodPost, path, nil).StatusCode }()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first pass never reached the network")
	}
	if resp := c.Post(path, nil); resp.StatusCode != http.StatusConflict {
		t.Fatalf("a second pass on the same node expected 409, got %d", resp.StatusCode)
	}
	close(release)
	if got := <-statuses; got != http.StatusOK {
		t.Fatalf("the first pass expected 200, got %d", got)
	}
	// The slot is given back, so the next pass is accepted.
	if resp := c.Post(path, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("after the pass finished expected 200, got %d", resp.StatusCode)
	}
}
