package api_test

import (
	"bufio"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
)

type nodeResp struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Hostname     string    `json:"hostname"`
	Status       string    `json:"status"`
	Online       bool      `json:"online"`
	ProfileCount int       `json:"profile_count"`
	MaxProfiles  int       `json:"max_profiles"`
	Dirty        bool      `json:"dirty"`
	AdTag        string    `json:"ad_tag"`
}

func createNode(t *testing.T, c *apitest.Client, host string) (nodeResp, string) {
	t.Helper()
	var out struct {
		Node           nodeResp `json:"node"`
		InstallCommand string   `json:"install_command"`
	}
	resp := c.Post("/api/v1/nodes", map[string]string{"name": host, "hostname": host, "acme_email": "a@b.co"})
	if resp.StatusCode != 201 {
		t.Fatalf("create node %d", resp.StatusCode)
	}
	c.JSON(resp, &out)
	return out.Node, out.InstallCommand
}

func TestCreateNodeCreatesDefaultProfileAndInstallCommand(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, cmd := createNode(t, c, "n1.test")
	if n.Status != "pending" || n.ProfileCount != 1 || n.MaxProfiles != 128 {
		t.Fatalf("node %+v", n)
	}
	if !strings.HasPrefix(cmd, "curl -fsSL http://panel.test/api/v1/install/") || !strings.HasSuffix(cmd, ".sh | sudo bash") {
		t.Fatalf("command %q", cmd)
	}
}

func TestCreateNodeValidation(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	resp := c.Post("/api/v1/nodes", map[string]string{"name": "x", "hostname": "Bad Host", "acme_email": "nope"})
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
	createNode(t, c, "dup.test")
	resp = c.Post("/api/v1/nodes", map[string]string{"name": "x", "hostname": "dup.test", "acme_email": "a@b.co"})
	if resp.StatusCode != 409 {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestNodeListGetPatchDelete(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	h.Mock.SetOnline(n.ID, true)

	var list struct {
		Items []nodeResp `json:"items"`
		Total int        `json:"total"`
	}
	c.JSON(c.Get("/api/v1/nodes"), &list)
	if list.Total != 1 || !list.Items[0].Online {
		t.Fatalf("list %+v", list)
	}
	var got nodeResp
	c.JSON(c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"name": "renamed", "max_profiles": 64}), &got)
	if got.Name != "renamed" || got.MaxProfiles != 64 {
		t.Fatalf("patch %+v", got)
	}
	if resp := c.Delete("/api/v1/nodes/" + n.ID.String()); resp.StatusCode != 204 {
		t.Fatalf("delete %d", resp.StatusCode)
	}
	if resp := c.Get("/api/v1/nodes/" + n.ID.String()); resp.StatusCode != 404 {
		t.Fatalf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

func TestNodePatchAdTag(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	if n.Dirty {
		t.Fatalf("freshly created node must not be dirty: %+v", n)
	}

	resp := c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"ad_tag": "not-a-tag"})
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422 for a malformed ad tag, got %d", resp.StatusCode)
	}

	tag := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var got nodeResp
	c.JSON(c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"ad_tag": tag}), &got)
	if got.AdTag != tag || !got.Dirty {
		t.Fatalf("patch %+v", got)
	}

	// Clearing it back to empty is a valid change too (turns the sponsor channel off).
	c.JSON(c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"ad_tag": ""}), &got)
	if got.AdTag != "" {
		t.Fatalf("ad tag not cleared: %+v", got)
	}
}

func TestNodeRegistrationSecret(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")

	var got struct {
		Secret string `json:"secret"`
	}
	resp := c.Get("/api/v1/nodes/" + n.ID.String() + "/registration-secret")
	if resp.StatusCode != 200 {
		t.Fatalf("registration-secret: %d", resp.StatusCode)
	}
	c.JSON(resp, &got)
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(got.Secret) {
		t.Fatalf("secret %q is not 32 lowercase hex characters", got.Secret)
	}

	h.CreateAdmin("v", "pass-123456", "viewer")
	viewer := h.Login("v", "pass-123456")
	if resp := viewer.Get("/api/v1/nodes/" + n.ID.String() + "/registration-secret"); resp.StatusCode != 403 {
		t.Fatalf("viewer: expected 403, got %d", resp.StatusCode)
	}
}

func TestNodeHealthStatsMetricsRestart(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	if resp := c.Get("/api/v1/nodes/" + n.ID.String() + "/health"); resp.StatusCode != 503 {
		t.Fatalf("offline health expected 503, got %d", resp.StatusCode)
	}
	h.Mock.SetOnline(n.ID, true)
	h.Mock.SetHealth(n.ID, nodedriver.HealthReport{RelayActive: true, Healthz: true, TProxyVersion: "52a5feb"})
	h.Mock.SetStats(n.ID, map[string]string{"active_connections": "7"})
	var health struct {
		RelayActive   bool   `json:"relay_active"`
		TProxyVersion string `json:"tproxy_version"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/health"), &health)
	if !health.RelayActive || health.TProxyVersion != "52a5feb" {
		t.Fatalf("health %+v", health)
	}
	var stats map[string]string
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/stats"), &stats)
	if stats["active_connections"] != "7" {
		t.Fatalf("stats %+v", stats)
	}
	resp := c.Get("/api/v1/nodes/" + n.ID.String() + "/metrics")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("metrics %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if resp := c.Post("/api/v1/nodes/"+n.ID.String()+"/restart", nil); resp.StatusCode != 204 {
		t.Fatalf("restart %d", resp.StatusCode)
	}
	if h.Mock.Restarts(n.ID) != 1 {
		t.Fatal("driver restart not called")
	}
}

func TestNodeLogsSSE(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	h.Mock.SetOnline(n.ID, true)
	resp := c.Get("/api/v1/nodes/" + n.ID.String() + "/logs?services=tproxy-server,mtproxy&lines=10")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("sse %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	defer resp.Body.Close() //nolint:errcheck
	var logEvents int
	var sawEnd bool
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		switch sc.Text() {
		case "event: log":
			logEvents++
		case "event: end":
			sawEnd = true
		}
	}
	if logEvents != 2 {
		t.Fatalf("log events = %d", logEvents)
	}
	if !sawEnd {
		t.Fatal("expected an event: end line")
	}
}

func TestViewerCannotCreateNode(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("v", "pass-123456", "viewer")
	c := h.Login("v", "pass-123456")
	if resp := c.Post("/api/v1/nodes", map[string]string{"name": "x", "hostname": "x.test", "acme_email": "a@b.co"}); resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// TestNodeListHealthMatchesHealthEndpoint: the list's `health` is the last heartbeat, and it
// must come out in the same snake_case shape as GET /nodes/{id}/health - the SPA has one
// NodeHealth type for both, and the row is stored with Go field names.
func TestNodeListHealthMatchesHealthEndpoint(t *testing.T) {
	h, c, n := ownerWithNode(t)
	raw, _ := json.Marshal(nodedriver.HealthReport{RelayActive: true, CPUPercent: 42.5, MemUsedPercent: 61, DiskUsedPercent: 12})
	if err := h.Store.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{ID: n.ID, Status: db.NodeStatusOnline, LastHealth: raw}); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Items []struct {
			Health map[string]any `json:"health"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/nodes"), &list)
	if len(list.Items) != 1 {
		t.Fatalf("list %+v", list)
	}
	got := list.Items[0].Health
	if got["cpu_percent"] != 42.5 || got["mem_used_percent"] != 61.0 || got["disk_used_percent"] != 12.0 || got["relay_active"] != true {
		t.Fatalf("list health %+v", got)
	}
	if _, pascal := got["CPUPercent"]; pascal {
		t.Fatalf("list health leaked the stored field names: %+v", got)
	}
}

// TestNodeListHealthCarriesDcConnectivity: the DC fields the heartbeat stores come out of the
// list's `health` in the spec's snake_case names, with an unmeasured DC kept known=false.
func TestNodeListHealthCarriesDcConnectivity(t *testing.T) {
	h, c, n := ownerWithNode(t)
	raw, _ := json.Marshal(nodedriver.HealthReport{
		RelayActive: true, DcDataAvailable: true, UpstreamHealthy: true, UpstreamFails: 2, EffectiveLatencyMs: 41.25,
		ConnectSuccessTotal: 58, ConnectFailTotal: 1, UpstreamLastCheckAgeSecs: 29,
		DCs: []nodedriver.DcLatency{
			{DC: 1, LatencyMs: 197.9, Known: true, IPPreference: "prefer_v4"},
			{DC: 4, Known: false, IPPreference: "prefer_v6"},
		},
	})
	if err := h.Store.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{ID: n.ID, Status: db.NodeStatusOnline, LastHealth: raw}); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Items []struct {
			Health map[string]any `json:"health"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/nodes"), &list)
	if len(list.Items) != 1 {
		t.Fatalf("list %+v", list)
	}
	got := list.Items[0].Health
	if got["dc_data_available"] != true || got["upstream_healthy"] != true || got["upstream_fails"] != 2.0 ||
		got["effective_latency_ms"] != 41.25 || got["connect_success_total"] != 58.0 || got["connect_fail_total"] != 1.0 ||
		got["upstream_last_check_age_secs"] != 29.0 {
		t.Fatalf("list health %+v", got)
	}
	dcs, _ := got["dcs"].([]any)
	if len(dcs) != 2 {
		t.Fatalf("dcs %+v", got["dcs"])
	}
	d1, _ := dcs[0].(map[string]any)
	if d1["dc"] != 1.0 || d1["latency_ms"] != 197.9 || d1["known"] != true || d1["ip_preference"] != "prefer_v4" {
		t.Fatalf("dc 1 %+v", d1)
	}
	d4, _ := dcs[1].(map[string]any)
	if d4["dc"] != 4.0 || d4["known"] != false || d4["latency_ms"] != 0.0 || d4["ip_preference"] != "prefer_v6" {
		t.Fatalf("dc 4 %+v", d4)
	}
	if _, pascal := got["DCs"]; pascal {
		t.Fatalf("list health leaked the stored field names: %+v", got)
	}
	// The single-node view goes through the same projection.
	var one struct {
		Health map[string]any `json:"health"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &one)
	if one.Health["dc_data_available"] != true || one.Health["effective_latency_ms"] != 41.25 {
		t.Fatalf("node health %+v", one.Health)
	}
}

// A tproxy node's heartbeat never carries DC data; the list must say so explicitly (false, with
// an empty dcs list) rather than omit the fields, so the SPA has one shape for both engines.
func TestNodeListHealthTproxyHasNoDcData(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	var created struct {
		Node nodeResp `json:"node"`
	}
	resp := c.Post("/api/v1/nodes", map[string]string{"name": "t1.test", "hostname": "t1.test", "acme_email": "a@b.co", "engine": "tproxy"})
	if resp.StatusCode != 201 {
		t.Fatalf("create tproxy node %d", resp.StatusCode)
	}
	c.JSON(resp, &created)
	n := created.Node
	raw, _ := json.Marshal(nodedriver.HealthReport{RelayActive: true, CPUPercent: 1})
	if err := h.Store.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{ID: n.ID, Status: db.NodeStatusOnline, LastHealth: raw}); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Items []struct {
			Engine string         `json:"engine"`
			Health map[string]any `json:"health"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/nodes"), &list)
	if len(list.Items) != 1 || list.Items[0].Engine != "tproxy" {
		t.Fatalf("list %+v", list)
	}
	got := list.Items[0].Health
	v, present := got["dc_data_available"]
	if !present || v != false {
		t.Fatalf("tproxy health must carry dc_data_available=false: %+v", got)
	}
	dcs, ok := got["dcs"].([]any)
	if !ok || len(dcs) != 0 {
		t.Fatalf("tproxy health dcs must be an empty list: %+v", got["dcs"])
	}
	if got["effective_latency_ms"] != 0.0 {
		t.Fatalf("tproxy health %+v", got)
	}
}
