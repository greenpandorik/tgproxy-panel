package api_test

import (
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
)

func TestListNodesCarriesProfileCounts(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n1, _ := createNode(t, c, "n1.test")
	n2, _ := createNode(t, c, "n2.test")
	// Two extra profiles on n1 via keys bound to it.
	for _, label := range []string{"a", "b"} {
		resp := c.Post("/api/v1/keys", map[string]any{"label": label, "type": "PERSONAL", "node_ids": []uuid.UUID{n1.ID}})
		if resp.StatusCode != 201 {
			t.Fatalf("create key %s: %d", label, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	var list struct {
		Items []nodeResp `json:"items"`
		Total int        `json:"total"`
	}
	c.JSON(c.Get("/api/v1/nodes"), &list)
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("list %+v", list)
	}
	got := map[uuid.UUID]int{}
	for _, it := range list.Items {
		got[it.ID] = it.ProfileCount
	}
	// Each node starts with its default profile; n1 gained two more.
	if got[n1.ID] != 3 || got[n2.ID] != 1 {
		t.Fatalf("profile counts: n1=%d (want 3), n2=%d (want 1)", got[n1.ID], got[n2.ID])
	}

	// The single-node projection must agree with the list projection.
	var single nodeResp
	c.JSON(c.Get("/api/v1/nodes/"+n1.ID.String()), &single)
	if single.ProfileCount != got[n1.ID] {
		t.Fatalf("GET /nodes/{id} says %d, list says %d", single.ProfileCount, got[n1.ID])
	}
}

func TestListKeysCarriesBindings(t *testing.T) {
	h, c, n1 := ownerWithNode(t)
	_ = h
	n2, _ := createNode(t, c, "n2.test")

	var bound, unbound keyResp
	c.JSON(c.Post("/api/v1/keys", map[string]any{"label": "both", "type": "SHARED", "node_ids": []uuid.UUID{n1.ID, n2.ID}}), &bound)
	c.JSON(c.Post("/api/v1/keys", map[string]any{"label": "one", "type": "PERSONAL", "node_ids": []uuid.UUID{n2.ID}}), &unbound)

	var list struct {
		Items []keyResp `json:"items"`
		Total int       `json:"total"`
	}
	c.JSON(c.Get("/api/v1/keys"), &list)
	if list.Total != 2 {
		t.Fatalf("total %d, want 2", list.Total)
	}
	byID := map[uuid.UUID][]uuid.UUID{}
	for _, k := range list.Items {
		for _, n := range k.Nodes {
			byID[k.ID] = append(byID[k.ID], n.NodeID)
		}
	}
	if len(byID[bound.ID]) != 2 {
		t.Fatalf("two-node key lists %v", byID[bound.ID])
	}
	if len(byID[unbound.ID]) != 1 || byID[unbound.ID][0] != n2.ID {
		t.Fatalf("one-node key lists %v, want [%s]", byID[unbound.ID], n2.ID)
	}

	// The per-key projection must agree with the batched one.
	var single keyResp
	c.JSON(c.Get("/api/v1/keys/"+bound.ID.String()), &single)
	if len(single.Nodes) != len(byID[bound.ID]) {
		t.Fatalf("GET /keys/{id} lists %d nodes, list lists %d", len(single.Nodes), len(byID[bound.ID]))
	}
}
