package api_test

import (
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
)

func listNodeNames(t *testing.T, c *apitest.Client) []string {
	t.Helper()
	var out struct {
		Items []nodeResp `json:"items"`
	}
	resp := c.Get("/api/v1/nodes")
	if resp.StatusCode != 200 {
		t.Fatalf("list nodes %d", resp.StatusCode)
	}
	c.JSON(resp, &out)
	names := make([]string, 0, len(out.Items))
	for _, n := range out.Items {
		names = append(names, n.Name)
	}
	return names
}

func sameNames(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
}

func reorderFixture(t *testing.T) (*apitest.Harness, *apitest.Client, map[string]uuid.UUID) {
	t.Helper()
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	ids := map[string]uuid.UUID{}
	for _, host := range []string{"a.test", "b.test", "c.test", "d.test"} {
		n, _ := createNode(t, c, host)
		ids[host] = n.ID
	}
	return h, c, ids
}

func TestReorderNodesPersistsAndListFollowsIt(t *testing.T) {
	_, c, ids := reorderFixture(t)
	sameNames(t, listNodeNames(t, c), "a.test", "b.test", "c.test", "d.test")

	resp := c.Put("/api/v1/nodes/order", map[string]any{"ids": []uuid.UUID{ids["c.test"], ids["a.test"], ids["d.test"], ids["b.test"]}})
	if resp.StatusCode != 200 {
		t.Fatalf("reorder %d", resp.StatusCode)
	}
	var out struct {
		IDs []uuid.UUID `json:"ids"`
	}
	c.JSON(resp, &out)
	if len(out.IDs) != 4 || out.IDs[0] != ids["c.test"] || out.IDs[3] != ids["b.test"] {
		t.Fatalf("returned order %v", out.IDs)
	}
	sameNames(t, listNodeNames(t, c), "c.test", "a.test", "d.test", "b.test")
}

func TestReorderNodesPartialListKeepsTheRestInOrder(t *testing.T) {
	_, c, ids := reorderFixture(t)
	resp := c.Put("/api/v1/nodes/order", map[string]any{"ids": []uuid.UUID{ids["d.test"], ids["b.test"]}})
	if resp.StatusCode != 200 {
		t.Fatalf("reorder %d", resp.StatusCode)
	}
	sameNames(t, listNodeNames(t, c), "d.test", "b.test", "a.test", "c.test")
}

func TestReorderNodesIgnoresUnknownIDs(t *testing.T) {
	_, c, ids := reorderFixture(t)
	resp := c.Put("/api/v1/nodes/order", map[string]any{"ids": []uuid.UUID{uuid.New(), ids["c.test"], uuid.New()}})
	if resp.StatusCode != 200 {
		t.Fatalf("reorder %d", resp.StatusCode)
	}
	sameNames(t, listNodeNames(t, c), "c.test", "a.test", "b.test", "d.test")
}

func TestReorderNodesRejectsBadLists(t *testing.T) {
	_, c, ids := reorderFixture(t)
	many := make([]uuid.UUID, 1001)
	for i := range many {
		many[i] = uuid.New()
	}
	for name, body := range map[string]any{
		"duplicates": map[string]any{"ids": []uuid.UUID{ids["a.test"], ids["b.test"], ids["a.test"]}},
		"empty":      map[string]any{"ids": []uuid.UUID{}},
		"missing":    map[string]any{},
		"too many":   map[string]any{"ids": many},
		"not an id":  map[string]any{"ids": []string{"nope"}},
	} {
		if resp := c.Put("/api/v1/nodes/order", body); resp.StatusCode != 400 {
			t.Errorf("%s: got %d, want 400", name, resp.StatusCode)
		}
	}
	sameNames(t, listNodeNames(t, c), "a.test", "b.test", "c.test", "d.test")
}

func TestReorderNodesNeedsAWriter(t *testing.T) {
	h, c, ids := reorderFixture(t)
	h.CreateAdmin("v", "pass-123456", "viewer")
	viewer := h.Login("v", "pass-123456")
	resp := viewer.Put("/api/v1/nodes/order", map[string]any{"ids": []uuid.UUID{ids["d.test"]}})
	if resp.StatusCode != 403 {
		t.Fatalf("viewer got %d, want 403", resp.StatusCode)
	}
	sameNames(t, listNodeNames(t, c), "a.test", "b.test", "c.test", "d.test")
}

func TestUserServersFollowTheOrder(t *testing.T) {
	_, c, ids := reorderFixture(t)
	nodeIDs := []string{ids["a.test"].String(), ids["b.test"].String(), ids["c.test"].String()}
	var k keyResp
	resp := c.Post("/api/v1/keys", map[string]any{"label": "Ivan", "type": "PERSONAL", "carrier_mode": "https", "node_ids": nodeIDs})
	if resp.StatusCode != 201 {
		t.Fatalf("create key %d", resp.StatusCode)
	}
	c.JSON(resp, &k)
	if resp := c.Put("/api/v1/nodes/order", map[string]any{"ids": []uuid.UUID{ids["c.test"], ids["a.test"]}}); resp.StatusCode != 200 {
		t.Fatalf("reorder %d", resp.StatusCode)
	}
	var got keyResp
	c.JSON(c.Get("/api/v1/keys/"+k.ID.String()), &got)
	want := []uuid.UUID{ids["c.test"], ids["a.test"], ids["b.test"]}
	if len(got.Nodes) != len(want) {
		t.Fatalf("nodes %+v", got.Nodes)
	}
	for i, n := range got.Nodes {
		if n.NodeID != want[i] {
			t.Fatalf("node %d is %s, want %s", i, n.NodeID, want[i])
		}
	}
}

func TestNewNodeGoesLast(t *testing.T) {
	_, c, ids := reorderFixture(t)
	resp := c.Put("/api/v1/nodes/order", map[string]any{"ids": []uuid.UUID{ids["d.test"], ids["c.test"], ids["b.test"], ids["a.test"]}})
	if resp.StatusCode != 200 {
		t.Fatalf("reorder %d", resp.StatusCode)
	}
	createNode(t, c, "e.test")
	sameNames(t, listNodeNames(t, c), "d.test", "c.test", "b.test", "a.test", "e.test")
}
