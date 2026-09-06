package api_test

import (
	"bufio"
	"strings"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/nodedriver"
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
