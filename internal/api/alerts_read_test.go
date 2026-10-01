package api_test

import (
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/store/db"
)

type alertList struct {
	Items []struct {
		ID   int64  `json:"id"`
		Kind string `json:"kind"`
	} `json:"items"`
}

func TestResolveAlertsMarksOnlyTheListedOnesRead(t *testing.T) {
	h, c, n := ownerWithNode(t)
	ctx := t.Context()
	raise := func(kind string) int64 {
		t.Helper()
		a, err := h.Store.Q.InsertAlert(ctx, db.InsertAlertParams{NodeID: uuid.NullUUID{UUID: n.ID, Valid: true}, Kind: kind, Message: kind})
		if err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	first, second := raise("node_offline"), raise("apply_failed")

	resp := c.Post("/api/v1/alerts/resolve", map[string]any{"ids": []int64{first, second, 999999}})
	if resp.StatusCode != 200 {
		t.Fatalf("resolve %d", resp.StatusCode)
	}
	var out struct {
		Resolved int `json:"resolved"`
	}
	c.JSON(resp, &out)
	if out.Resolved != 2 {
		t.Fatalf("resolved %d, want 2", out.Resolved)
	}

	later := raise("config_deferred")
	var list alertList
	c.JSON(c.Get("/api/v1/alerts"), &list)
	if len(list.Items) != 1 || list.Items[0].ID != later {
		t.Fatalf("inbox %+v, want only the alert raised after the read", list.Items)
	}

	open, err := h.Store.Q.ListOpenAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("a read alert must stay open until its problem clears, got %d open", len(open))
	}

	resp = c.Post("/api/v1/alerts/resolve", map[string]any{"ids": []int64{first}})
	c.JSON(resp, &out)
	if out.Resolved != 0 {
		t.Fatalf("reading an alert twice counted %d", out.Resolved)
	}
}

func TestResolveAlertsRejectsBadLists(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	many := make([]int64, 501)
	for i := range many {
		many[i] = int64(i + 1)
	}
	for name, body := range map[string]any{
		"empty":    map[string]any{"ids": []int64{}},
		"missing":  map[string]any{},
		"too many": map[string]any{"ids": many},
		"not ids":  map[string]any{"ids": []string{"x"}},
	} {
		if resp := c.Post("/api/v1/alerts/resolve", body); resp.StatusCode != 400 {
			t.Errorf("%s: got %d, want 400", name, resp.StatusCode)
		}
	}
	h.CreateAdmin("v", "pass-123456", "viewer")
	viewer := h.Login("v", "pass-123456")
	if resp := viewer.Post("/api/v1/alerts/resolve", map[string]any{"ids": []int64{1}}); resp.StatusCode != 403 {
		t.Fatalf("viewer got %d, want 403", resp.StatusCode)
	}
}

func TestResolveOneAlertMarksItRead(t *testing.T) {
	h, c, n := ownerWithNode(t)
	a, err := h.Store.Q.InsertAlert(t.Context(), db.InsertAlertParams{NodeID: uuid.NullUUID{UUID: n.ID, Valid: true}, Kind: "node_offline", Message: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if resp := c.Post("/api/v1/alerts/"+itoa(a.ID)+"/resolve", nil); resp.StatusCode != 200 {
		t.Fatalf("resolve %d", resp.StatusCode)
	}
	var list alertList
	c.JSON(c.Get("/api/v1/alerts"), &list)
	if len(list.Items) != 0 {
		t.Fatalf("inbox %+v", list.Items)
	}
}
