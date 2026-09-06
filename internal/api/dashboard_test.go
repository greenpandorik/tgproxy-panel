package api_test

import (
	"io"
	"strings"
	"testing"

	"tgwebproxy/internal/store/db"
)

func TestDashboardSummaryAndMetrics(t *testing.T) {
	h, c, n := ownerWithNode(t)
	c.Post("/api/v1/keys", map[string]any{"label": "k", "type": "SHARED", "carrier_mode": "https", "node_ids": []string{n.ID.String()}})
	_ = h.Store.Q.InsertSnapshot(t.Context(), db.InsertSnapshotParams{NodeID: n.ID, SessionsLive: 3, StreamsLive: 9, BytesUp: 10, BytesDown: 20, MtproxyRaw: []byte("{}")})
	var sum struct {
		Nodes        map[string]int `json:"nodes"`
		Keys         map[string]int `json:"keys"`
		SessionsLive int            `json:"sessions_live"`
		StreamsLive  int            `json:"streams_live"`
	}
	c.JSON(c.Get("/api/v1/dashboard/summary"), &sum)
	if sum.Nodes["total"] != 1 || sum.Nodes["pending"] != 1 || sum.Keys["pending"] != 1 || sum.SessionsLive != 3 || sum.StreamsLive != 9 {
		t.Fatalf("summary %+v", sum)
	}
	resp := h.Anonymous().Get("/metrics")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `tgwp_nodes{status="pending"} 1`) || !strings.Contains(string(body), "tgwp_node_sessions_live") {
		t.Fatalf("metrics %d: %.300s", resp.StatusCode, body)
	}
	var series struct {
		Points []map[string]any `json:"points"`
	}
	c.JSON(c.Get("/api/v1/monitoring/nodes/"+n.ID.String()+"/series"), &series)
	if len(series.Points) != 1 {
		t.Fatalf("series %+v", series)
	}
	var audit struct {
		Total int `json:"total"`
	}
	c.JSON(c.Get("/api/v1/audit"), &audit)
	if audit.Total < 2 {
		t.Fatalf("audit total %d", audit.Total)
	}
	if resp := c.Put("/api/v1/settings", map[string]any{"apply_interval": 30, "offline_after": 120}); resp.StatusCode != 200 {
		t.Fatalf("settings %d", resp.StatusCode)
	}
	var settings struct {
		ApplyInterval int `json:"apply_interval"`
	}
	c.JSON(c.Get("/api/v1/settings"), &settings)
	if settings.ApplyInterval != 30 {
		t.Fatalf("settings %+v", settings)
	}
}
