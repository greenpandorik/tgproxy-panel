package api_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
)

type keyStatsResp struct {
	Nodes []struct {
		NodeID   uuid.UUID `json:"node_id"`
		NodeName string    `json:"node_name"`
		Points   []struct {
			T           time.Time `json:"t"`
			Connections int32     `json:"connections"`
			TotalOctets int64     `json:"total_octets"`
		} `json:"points"`
	} `json:"nodes"`
	Totals struct {
		ConnectionsNow int32 `json:"connections_now"`
		OctetsDelta    int64 `json:"octets_delta"`
	} `json:"totals"`
}

// seedKeyStats writes one key_stats_snapshots row at an explicit time; the generated insert
// always stamps now(), and these tests need a series.
func seedKeyStats(t *testing.T, h *apitest.Harness, keyID, nodeID uuid.UUID, at time.Time, conns int, octets int64) {
	t.Helper()
	_, err := h.Store.Pool.Exec(context.Background(),
		`INSERT INTO key_stats_snapshots (access_key_id, node_id, taken_at, connections, total_octets, active_ips)
		 VALUES ($1, $2, $3, $4, $5, 1)`, keyID, nodeID, at, conns, octets)
	if err != nil {
		t.Fatal(err)
	}
}

func keyOnNode(t *testing.T, c *apitest.Client, nodeID uuid.UUID) keyResp {
	t.Helper()
	var k keyResp
	resp := c.Post("/api/v1/keys", map[string]any{"label": "Ivan", "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{nodeID.String()}})
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create key %d %s", resp.StatusCode, b)
	}
	c.JSON(resp, &k)
	return k
}

func TestKeyStatsSeriesAndTotals(t *testing.T) {
	h, c, n := ownerWithNode(t)
	k := keyOnNode(t, c, n.ID)
	now := time.Now().UTC()
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-2*time.Hour), 1, 100)
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-1*time.Hour), 4, 1000)
	// Outside the default 24h window: it must not move the totals.
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-40*time.Hour), 9, 10)

	var got keyStatsResp
	c.JSON(c.Get("/api/v1/keys/"+k.ID.String()+"/stats"), &got)
	if len(got.Nodes) != 1 || got.Nodes[0].NodeID != n.ID || got.Nodes[0].NodeName != "n1.test" {
		t.Fatalf("nodes: %+v", got.Nodes)
	}
	if len(got.Nodes[0].Points) != 2 {
		t.Fatalf("points: %+v", got.Nodes[0].Points)
	}
	if got.Nodes[0].Points[1].Connections != 4 || got.Nodes[0].Points[1].TotalOctets != 1000 {
		t.Fatalf("last point: %+v", got.Nodes[0].Points[1])
	}
	// connections_now is the newest reading; octets_delta is the sum of the positive
	// step-to-step deltas per node.
	if got.Totals.ConnectionsNow != 4 || got.Totals.OctetsDelta != 900 {
		t.Fatalf("totals: %+v", got.Totals)
	}
}

// A key with no telemt node behind it answers with an empty series rather than an error, so
// the drawer renders the same for both engines.
func TestKeyStatsEmpty(t *testing.T) {
	_, c, n := ownerWithNode(t)
	k := keyOnNode(t, c, n.ID)
	var got keyStatsResp
	resp := c.Get("/api/v1/keys/" + k.ID.String() + "/stats")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	c.JSON(resp, &got)
	if len(got.Nodes) != 0 || got.Totals.OctetsDelta != 0 || got.Totals.ConnectionsNow != 0 {
		t.Fatalf("empty stats: %+v", got)
	}
}

func TestKeyStatsRangeValidation(t *testing.T) {
	_, c, n := ownerWithNode(t)
	k := keyOnNode(t, c, n.ID)
	base := "/api/v1/keys/" + k.ID.String() + "/stats"
	if resp := c.Get(base + "?from=nonsense"); resp.StatusCode != 400 {
		t.Fatalf("bad from: %d", resp.StatusCode)
	}
	from := time.Now().Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339)
	if resp := c.Get(base + "?from=" + from); resp.StatusCode != 400 {
		t.Fatalf("range wider than retention must be refused: %d", resp.StatusCode)
	}
	if resp := c.Get(base + "?node=" + uuid.NewString()); resp.StatusCode != 200 {
		t.Fatalf("unknown query params must be ignored: %d", resp.StatusCode)
	}
}

// Statistics are readable by every authenticated role, viewers included.
func TestKeyStatsReadableByViewer(t *testing.T) {
	h, c, n := ownerWithNode(t)
	k := keyOnNode(t, c, n.ID)
	seedKeyStats(t, h, k.ID, n.ID, time.Now().UTC().Add(-time.Hour), 2, 500)
	h.CreateAdmin("view", "pass-123456", "viewer")
	vc := h.Login("view", "pass-123456")
	var got keyStatsResp
	resp := vc.Get("/api/v1/keys/" + k.ID.String() + "/stats")
	if resp.StatusCode != 200 {
		t.Fatalf("viewer got %d", resp.StatusCode)
	}
	vc.JSON(resp, &got)
	if len(got.Nodes) != 1 || got.Totals.ConnectionsNow != 2 {
		t.Fatalf("viewer stats: %+v", got)
	}
}

func TestKeyStatsUnknownKey(t *testing.T) {
	_, c, _ := ownerWithNode(t)
	if resp := c.Get("/api/v1/keys/" + uuid.NewString() + "/stats"); resp.StatusCode != 404 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestKeysListCarriesTraffic30d(t *testing.T) {
	h, c, n := ownerWithNode(t)
	k := keyOnNode(t, c, n.ID)
	now := time.Now().UTC()
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-10*24*time.Hour), 0, 1_000)
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-time.Hour), 3, 6_000)
	// Older than the 30 day window: outside it entirely, so it cannot lower the first reading.
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-40*24*time.Hour), 0, 500_000)

	var list struct {
		Items []struct {
			ID         uuid.UUID `json:"id"`
			Traffic30d int64     `json:"traffic_30d"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/keys"), &list)
	if len(list.Items) != 1 || list.Items[0].ID != k.ID {
		t.Fatalf("list: %+v", list.Items)
	}
	if list.Items[0].Traffic30d != 5_000 {
		t.Fatalf("traffic_30d = %d, want 5000", list.Items[0].Traffic30d)
	}
	var single struct {
		Traffic30d int64 `json:"traffic_30d"`
	}
	c.JSON(c.Get("/api/v1/keys/"+k.ID.String()), &single)
	if single.Traffic30d != 5_000 {
		t.Fatalf("single key traffic_30d = %d", single.Traffic30d)
	}
}

// I2: total_octets is a process-scoped counter, so it restarts at zero when telemt does.
// Summing positive consecutive deltas keeps the traffic on both sides of the restart; taking
// last - first would read the window as negative and report zero for the whole 30 days.
func TestKeysListTrafficSurvivesACounterReset(t *testing.T) {
	h, c, n := ownerWithNode(t)
	k := keyOnNode(t, c, n.ID)
	now := time.Now().UTC()
	// Before the restart: 1000 -> 9000 (8000 octets).
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-4*time.Hour), 1, 1_000)
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-3*time.Hour), 2, 9_000)
	// telemt restarts; the counter starts again from zero and reaches 500.
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-2*time.Hour), 1, 0)
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-1*time.Hour), 3, 500)

	var list struct {
		Items []struct {
			ID         uuid.UUID `json:"id"`
			Traffic30d int64     `json:"traffic_30d"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/keys"), &list)
	if len(list.Items) != 1 {
		t.Fatalf("list: %+v", list.Items)
	}
	// 8000 across the restart-free run plus 500 after it; only the step spanning the reset
	// is lost, never the whole window.
	if list.Items[0].Traffic30d != 8_500 {
		t.Fatalf("traffic_30d = %d, want 8500", list.Items[0].Traffic30d)
	}
}

// I2, the same rule in the drawer: the list column and the per-key endpoint must agree.
func TestKeyStatsOctetsDeltaSurvivesACounterReset(t *testing.T) {
	h, c, n := ownerWithNode(t)
	k := keyOnNode(t, c, n.ID)
	now := time.Now().UTC()
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-4*time.Hour), 1, 1_000)
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-3*time.Hour), 2, 9_000)
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-2*time.Hour), 1, 0)
	seedKeyStats(t, h, k.ID, n.ID, now.Add(-1*time.Hour), 3, 500)

	var got keyStatsResp
	c.JSON(c.Get("/api/v1/keys/"+k.ID.String()+"/stats"), &got)
	if len(got.Nodes) != 1 || len(got.Nodes[0].Points) != 4 {
		t.Fatalf("series: %+v", got.Nodes)
	}
	if got.Totals.OctetsDelta != 8_500 {
		t.Fatalf("octets_delta = %d, want 8500", got.Totals.OctetsDelta)
	}
	if got.Totals.ConnectionsNow != 3 {
		t.Fatalf("connections_now = %d, want 3", got.Totals.ConnectionsNow)
	}
}

// M6: a wide range is bucketed by the database, so one node's series comes back already bounded
// instead of every raw row being loaded into Go. A 31-day window at one row a minute is ~45k
// rows; the response must hold at most maxKeyStatsPoints (1000) of them.
func TestKeyStatsBucketsAWideRange(t *testing.T) {
	h, c, n := ownerWithNode(t)
	k := keyOnNode(t, c, n.ID)
	now := time.Now().UTC()
	// 400 readings a minute apart, i.e. finer than the bucket width a 10-day window gets.
	for i := range 400 {
		seedKeyStats(t, h, k.ID, n.ID, now.Add(-time.Duration(400-i)*time.Minute), 1, int64(i)*10)
	}
	from := now.Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	var got keyStatsResp
	c.JSON(c.Get("/api/v1/keys/"+k.ID.String()+"/stats?from="+from), &got)
	if len(got.Nodes) != 1 {
		t.Fatalf("nodes: %+v", got.Nodes)
	}
	pts := got.Nodes[0].Points
	if len(pts) == 0 || len(pts) >= 400 {
		t.Fatalf("a wide range must be bucketed, got %d points for 400 rows", len(pts))
	}
	// The bucket maxima are the closing counter values, so the traffic is essentially
	// unchanged: only the readings inside the *first* bucket are folded away, which is at
	// most one bucket's worth of octets out of 3990.
	if d := got.Totals.OctetsDelta; d < 3_800 || d > 3_990 {
		t.Fatalf("octets_delta = %d, want ~3990", d)
	}
}
