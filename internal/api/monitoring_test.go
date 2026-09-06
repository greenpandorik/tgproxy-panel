package api_test

import (
	"testing"
	"time"

	"tgwebproxy/internal/store/db"
)

func TestMonitoringOverviewRates(t *testing.T) {
	h, c, n := ownerWithNode(t)
	base := time.Now().Add(-10 * time.Minute)
	for i, up := range []int64{1000, 4000, 2000} { // third point simulates a counter reset
		_ = h.Store.Q.InsertSnapshot(t.Context(), db.InsertSnapshotParams{NodeID: n.ID, SessionsLive: int32(i + 1), StreamsLive: 2, BytesUp: up, BytesDown: up * 2, MtproxyRaw: []byte("{}")})
		_, _ = h.Store.Pool.Exec(t.Context(), `UPDATE node_stats_snapshots SET taken_at = $1 WHERE node_id = $2 AND taken_at > $1`, base.Add(time.Duration(i)*time.Minute), n.ID)
	}
	var out struct {
		Nodes []struct {
			NodeID string `json:"node_id"`
		} `json:"nodes"`
		Series map[string][]struct {
			SessionsLive int     `json:"sessions_live"`
			BytesUpRate  float64 `json:"bytes_up_rate"`
		} `json:"series"`
	}
	c.JSON(c.Get("/api/v1/monitoring/overview?from="+base.Add(-time.Minute).Format(time.RFC3339)+"&to="+time.Now().Format(time.RFC3339)), &out)
	pts := out.Series[n.ID.String()]
	if len(out.Nodes) != 1 || len(pts) != 3 {
		t.Fatalf("overview %+v", out)
	}
	if pts[0].BytesUpRate != 0 || pts[1].BytesUpRate != 50 || pts[2].BytesUpRate != 0 {
		t.Fatalf("rates %+v (want 0, 50 B/s over 60s, 0 after reset)", pts)
	}
}

// TestMonitoringRangeIsClamped covers I3: both monitoring reads are mounted for every role
// and the SPA polls the overview every 60s, so an unbounded `from` let any authenticated
// user make the server materialise every retained snapshot for every node, repeatedly.
// Retention is 30 days, so anything past 31 is asking for rows that do not exist.
func TestMonitoringRangeIsClamped(t *testing.T) {
	_, c, n := ownerWithNode(t)
	to := time.Now()
	wide := "from=" + to.Add(-40*24*time.Hour).Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339)
	ok := "from=" + to.Add(-30*24*time.Hour).Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339)

	for _, path := range []string{"/api/v1/monitoring/overview?", "/api/v1/monitoring/nodes/" + n.ID.String() + "/series?"} {
		resp := c.Get(path + wide)
		if resp.StatusCode != 400 {
			t.Fatalf("%s: 40-day range got %d, want 400", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
		resp = c.Get(path + ok)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: 30-day range got %d, want 200", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
	// The default window (no from/to) must stay inside the clamp.
	resp := c.Get("/api/v1/monitoring/overview")
	if resp.StatusCode != 200 {
		t.Fatalf("default window got %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestMonitoringOverviewBucketsInSQL covers the other half of I3. A wide step used to be
// honoured by *sampling* in Go - one raw 60s rate every step seconds, with every other
// snapshot read, grouped and then thrown away. It now aggregates in the database: the row
// count is O(range/step) per node and each point summarises its whole bucket.
func TestMonitoringOverviewBucketsInSQL(t *testing.T) {
	h, c, n := ownerWithNode(t)
	// 20 snapshots, one per minute, counters climbing 60 bytes/minute (= 1 B/s).
	base := time.Now().Add(-25 * time.Minute).Truncate(time.Minute)
	for i := 0; i < 20; i++ {
		_ = h.Store.Q.InsertSnapshot(t.Context(), db.InsertSnapshotParams{
			NodeID: n.ID, SessionsLive: 4, StreamsLive: 8,
			BytesUp: int64(i) * 60, BytesDown: int64(i) * 120, MtproxyRaw: []byte("{}"),
		})
		_, _ = h.Store.Pool.Exec(t.Context(),
			`UPDATE node_stats_snapshots SET taken_at = $1 WHERE node_id = $2 AND taken_at > $1`,
			base.Add(time.Duration(i)*time.Minute), n.ID)
	}
	var out struct {
		Series map[string][]struct {
			T             time.Time `json:"t"`
			SessionsLive  int       `json:"sessions_live"`
			StreamsLive   int       `json:"streams_live"`
			BytesUpRate   float64   `json:"bytes_up_rate"`
			BytesDownRate float64   `json:"bytes_down_rate"`
		} `json:"series"`
	}
	q := "from=" + base.Add(-time.Minute).Format(time.RFC3339) + "&to=" + time.Now().Format(time.RFC3339)

	// step=60 is at the threshold: raw rows, one per minute.
	c.JSON(c.Get("/api/v1/monitoring/overview?"+q+"&step=60"), &out)
	if got := len(out.Series[n.ID.String()]); got != 20 {
		t.Fatalf("step=60 returned %d points, want the 20 raw snapshots", got)
	}

	// step=300 buckets 5 minutes at a time: at most 5 points for a 20-minute span.
	c.JSON(c.Get("/api/v1/monitoring/overview?"+q+"&step=300"), &out)
	pts := out.Series[n.ID.String()]
	if len(pts) < 3 || len(pts) > 5 {
		t.Fatalf("step=300 returned %d points, want 4 (+/-1) five-minute buckets: %+v", len(pts), pts)
	}
	for i, p := range pts {
		if p.SessionsLive != 4 || p.StreamsLive != 8 {
			t.Fatalf("bucket %d averaged gauges wrongly: %+v", i, p)
		}
		if i == 0 {
			continue
		}
		// Counters climb 1 B/s up and 2 B/s down throughout, so every bucket-to-bucket
		// rate must reproduce that regardless of the bucket width - including the last,
		// partially-filled bucket, which is why a bucket's timestamp is its newest sample
		// rather than the bucket boundary.
		if p.BytesUpRate != 1 || p.BytesDownRate != 2 {
			t.Fatalf("bucket %d rates = %v/%v, want 1/2 B/s", i, p.BytesUpRate, p.BytesDownRate)
		}
	}
}
