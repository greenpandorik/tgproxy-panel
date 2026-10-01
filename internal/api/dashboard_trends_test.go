package api_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
)

type trendsResp struct {
	People []struct {
		T            time.Time `json:"t"`
		PeopleOnline int       `json:"people_online"`
	} `json:"people"`
	Traffic24h     *int64 `json:"traffic_24h"`
	TrafficPrev24h *int64 `json:"traffic_prev_24h"`
}

func trafficAt(t *testing.T, h *apitest.Harness, node uuid.UUID, ago time.Duration, up, down int64) {
	t.Helper()
	_, err := h.Store.Pool.Exec(t.Context(),
		`INSERT INTO node_stats_snapshots (node_id, taken_at, sessions_live, streams_live, bytes_up, bytes_down, sessions_created, limit_hits)
		 VALUES ($1, now() - $2::interval, 0, 0, $3, $4, 0, 0)`, node, ago.String(), up, down)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDashboardTrendsTrafficWithoutAFullDayBefore(t *testing.T) {
	h, c, n := ownerWithNode(t)
	trafficAt(t, h, n.ID, 30*time.Hour, 0, 100)
	trafficAt(t, h, n.ID, 20*time.Hour, 0, 300)
	trafficAt(t, h, n.ID, 10*time.Hour, 0, 50)
	trafficAt(t, h, n.ID, time.Hour, 10, 150)

	var out trendsResp
	c.JSON(c.Get("/api/v1/dashboard/trends"), &out)
	if out.Traffic24h == nil || *out.Traffic24h != 200+0+110 {
		t.Fatalf("traffic %v, want the steps since a day ago with the restart left out", out.Traffic24h)
	}
	if out.TrafficPrev24h != nil {
		t.Fatalf("history starts 30h ago, so the day before is not covered: %v", *out.TrafficPrev24h)
	}
}

func TestDashboardTrendsComparesWithTheDayBefore(t *testing.T) {
	h, c, n := ownerWithNode(t)
	trafficAt(t, h, n.ID, 48*time.Hour-time.Minute, 0, 0)
	trafficAt(t, h, n.ID, 30*time.Hour, 0, 400)
	trafficAt(t, h, n.ID, 12*time.Hour, 0, 900)

	var out trendsResp
	c.JSON(c.Get("/api/v1/dashboard/trends"), &out)
	if out.Traffic24h == nil || *out.Traffic24h != 500 || out.TrafficPrev24h == nil || *out.TrafficPrev24h != 400 {
		t.Fatalf("traffic %v / %v, want 500 / 400", out.Traffic24h, out.TrafficPrev24h)
	}
}

func TestDashboardTrendsWithNoHistory(t *testing.T) {
	_, c, _ := ownerWithNode(t)
	var out trendsResp
	c.JSON(c.Get("/api/v1/dashboard/trends"), &out)
	if out.Traffic24h != nil || out.TrafficPrev24h != nil || len(out.People) != 0 {
		t.Fatalf("trends %+v, want nothing", out)
	}
}

func TestDashboardTrendsPeopleSeriesIsBucketed(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	_, err := h.Store.Pool.Exec(t.Context(),
		`INSERT INTO fleet_stats_snapshots (taken_at, people_online, people_15m, connections)
		 SELECT now() - (g || ' minutes')::interval, g % 7, 0, 0 FROM generate_series(1, 1500) AS g`)
	if err != nil {
		t.Fatal(err)
	}
	var out trendsResp
	c.JSON(c.Get("/api/v1/dashboard/trends"), &out)
	if len(out.People) < 48 || len(out.People) > 96 {
		t.Fatalf("people series has %d points, want 48..96", len(out.People))
	}
	for i := 1; i < len(out.People); i++ {
		if !out.People[i].T.After(out.People[i-1].T) {
			t.Fatalf("points out of order at %d", i)
		}
	}
	if since := time.Since(out.People[0].T); since > 24*time.Hour {
		t.Fatalf("first point is %s old", since)
	}
}
