package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/nodedriver"
)

type capacityNode struct {
	WebCapacity          *int           `json:"web_capacity"`
	WebCapacitySupported bool           `json:"web_capacity_supported"`
	Dirty                bool           `json:"dirty"`
	Health               map[string]any `json:"health"`
}

func TestNodePatchWebCapacity(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	path := "/api/v1/nodes/" + n.ID.String()

	var got capacityNode
	c.JSON(c.Get(path), &got)
	if got.WebCapacity != nil || got.WebCapacitySupported {
		t.Fatalf("a node no agent has reported for has no capacity choice: %+v", got)
	}

	resp := c.Patch(path, map[string]any{"web_capacity": 1024})
	if resp.StatusCode != 422 {
		t.Fatalf("an unknown agent cannot apply a preset, got %d", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	c.JSON(resp, &body)
	if !strings.Contains(body.Error.Fields["web_capacity"], "agent 2.17.4 or newer") {
		t.Fatalf("the refusal names the agent it needs: %+v", body)
	}

	if _, err := h.Store.Pool.Exec(t.Context(), `UPDATE nodes SET telemt_version='3.5.14', agent_version='2.17.4' WHERE id=$1`, n.ID); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []any{0, 300, 2048, "512"} {
		if resp := c.Patch(path, map[string]any{"web_capacity": bad}); resp.StatusCode != 422 && resp.StatusCode != 400 {
			t.Fatalf("%v must be refused, got %d", bad, resp.StatusCode)
		}
	}
	c.JSON(c.Patch(path, map[string]any{"web_capacity": 512}), &got)
	if got.WebCapacity == nil || *got.WebCapacity != 512 || !got.Dirty || !got.WebCapacitySupported {
		t.Fatalf("patch %+v", got)
	}

	h.CreateAdmin("v", "pass-123456", "viewer")
	viewer := h.Login("v", "pass-123456")
	if resp := viewer.Patch(path, map[string]any{"web_capacity": 1024}); resp.StatusCode != 403 {
		t.Fatalf("viewer: expected 403, got %d", resp.StatusCode)
	}

	if _, err := h.Store.Pool.Exec(t.Context(), `UPDATE nodes SET dirty=false, agent_version='2.17.3' WHERE id=$1`, n.ID); err != nil {
		t.Fatal(err)
	}
	c.JSON(c.Patch(path, map[string]any{"web_capacity": 512}), &got)
	if got.Dirty || got.WebCapacity == nil || *got.WebCapacity != 512 {
		t.Fatalf("resending the stored preset changes nothing, even on an older agent: %+v", got)
	}

	health, _ := json.Marshal(nodedriver.HealthReport{WebCapacity: 512, MaxConnections: 20000})
	if _, err := h.Store.Pool.Exec(t.Context(), `UPDATE nodes SET last_health=$2 WHERE id=$1`, n.ID, health); err != nil {
		t.Fatal(err)
	}
	c.JSON(c.Get(path), &got)
	if got.Health["web_capacity"] != float64(512) || got.Health["max_connections"] != float64(20000) {
		t.Fatalf("health %+v", got.Health)
	}
}
