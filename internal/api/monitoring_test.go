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

// TestMonitoringLoadSeries: both monitoring reads carry the server load. The per-node series
// returns it per snapshot; the overview averages it per bucket like the other gauges.
func TestMonitoringLoadSeries(t *testing.T) {
	h, c, n := ownerWithNode(t)
	// Aligned to a five-minute boundary so that base and base+1m never straddle
	// a bucket, whatever the wall clock says when the test runs.
	base := time.Now().Add(-7 * time.Minute).Truncate(5 * time.Minute)
	for i, cpu := range []float32{20, 40} {
		_ = h.Store.Q.InsertSnapshot(t.Context(), db.InsertSnapshotParams{
			NodeID: n.ID, SessionsLive: 1, MtproxyRaw: []byte("{}"),
			CpuPercent: cpu, MemUsedPercent: cpu + 10, DiskUsedPercent: 5,
		})
		_, _ = h.Store.Pool.Exec(t.Context(),
			`UPDATE node_stats_snapshots SET taken_at = $1 WHERE node_id = $2 AND taken_at > $1`,
			base.Add(time.Duration(i)*time.Minute), n.ID)
	}
	q := "from=" + base.Format(time.RFC3339) + "&to=" + time.Now().Format(time.RFC3339)

	var series struct {
		Points []struct {
			CPU  float32 `json:"cpu_percent"`
			Mem  float32 `json:"mem_used_percent"`
			Disk float32 `json:"disk_used_percent"`
		} `json:"points"`
	}
	c.JSON(c.Get("/api/v1/monitoring/nodes/"+n.ID.String()+"/series?"+q), &series)
	if len(series.Points) != 2 || series.Points[0].CPU != 20 || series.Points[1].CPU != 40 ||
		series.Points[1].Mem != 50 || series.Points[1].Disk != 5 {
		t.Fatalf("series load %+v", series.Points)
	}

	// Both snapshots fall into one five-minute bucket; the bucket reports their average.
	var overview struct {
		Series map[string][]struct {
			CPU  float32 `json:"cpu_percent"`
			Mem  float32 `json:"mem_used_percent"`
			Disk float32 `json:"disk_used_percent"`
		} `json:"series"`
	}
	c.JSON(c.Get("/api/v1/monitoring/overview?"+q+"&step=300"), &overview)
	pts := overview.Series[n.ID.String()]
	if len(pts) != 1 || pts[0].CPU != 30 || pts[0].Mem != 40 || pts[0].Disk != 5 {
		t.Fatalf("overview bucket load %+v (want cpu 30, mem 40, disk 5)", pts)
	}
	// Below the bucketing threshold the raw rows carry the load too.
	c.JSON(c.Get("/api/v1/monitoring/overview?"+q+"&step=60"), &overview)
	if pts = overview.Series[n.ID.String()]; len(pts) != 2 || pts[1].CPU != 40 {
		t.Fatalf("overview raw load %+v", pts)
	}
}

// TestMonitoringDcLatencySeries: both monitoring reads carry dc_latency. The per-node series
// returns each snapshot's object as stored; the overview averages per DC over the rows of the
// bucket that measured that DC - a row without the key is "unknown" and must not count as 0.
func TestMonitoringDcLatencySeries(t *testing.T) {
	h, c, n := ownerWithNode(t)
	base := time.Now().Add(-7 * time.Minute).Truncate(5 * time.Minute)
	for i, dc := range []string{`{"1": 100, "2": 30}`, `{"1": 200}`, `{}`} {
		_ = h.Store.Q.InsertSnapshot(t.Context(), db.InsertSnapshotParams{
			NodeID: n.ID, SessionsLive: 1, MtproxyRaw: []byte("{}"), DcLatency: []byte(dc),
		})
		_, _ = h.Store.Pool.Exec(t.Context(),
			`UPDATE node_stats_snapshots SET taken_at = $1 WHERE node_id = $2 AND taken_at > $1`,
			base.Add(time.Duration(i)*time.Minute), n.ID)
	}
	q := "from=" + base.Format(time.RFC3339) + "&to=" + time.Now().Format(time.RFC3339)

	type point struct {
		DcLatency map[string]float64 `json:"dc_latency"`
	}
	var series struct {
		Points []point `json:"points"`
	}
	c.JSON(c.Get("/api/v1/monitoring/nodes/"+n.ID.String()+"/series?"+q), &series)
	if len(series.Points) != 3 {
		t.Fatalf("series %+v", series.Points)
	}
	if p := series.Points[0].DcLatency; len(p) != 2 || p["1"] != 100 || p["2"] != 30 {
		t.Fatalf("series point 0 dc_latency %+v", p)
	}
	if p := series.Points[1].DcLatency; len(p) != 1 || p["1"] != 200 {
		t.Fatalf("series point 1 dc_latency %+v", p)
	}
	if p := series.Points[2].DcLatency; p == nil || len(p) != 0 {
		t.Fatalf("series point 2 dc_latency must be an empty object, got %+v", p)
	}

	// All three fall into one five-minute bucket: DC 1 averages its two readings, DC 2 keeps
	// its single reading rather than being averaged with a missing 0.
	var overview struct {
		Series map[string][]point `json:"series"`
	}
	c.JSON(c.Get("/api/v1/monitoring/overview?"+q+"&step=300"), &overview)
	pts := overview.Series[n.ID.String()]
	if len(pts) != 1 {
		t.Fatalf("overview buckets %+v", pts)
	}
	if p := pts[0].DcLatency; len(p) != 2 || p["1"] != 150 || p["2"] != 30 {
		t.Fatalf("overview bucket dc_latency %+v (want 1: 150, 2: 30)", p)
	}
	// Below the bucketing threshold the raw rows carry the object too.
	c.JSON(c.Get("/api/v1/monitoring/overview?"+q+"&step=60"), &overview)
	pts = overview.Series[n.ID.String()]
	if len(pts) != 3 || pts[1].DcLatency["1"] != 200 || len(pts[2].DcLatency) != 0 || pts[2].DcLatency == nil {
		t.Fatalf("overview raw dc_latency %+v", pts)
	}
}

// A bucket none of whose rows measured a DC reports an empty object, not null.
func TestMonitoringDcLatencyEmptyBucket(t *testing.T) {
	h, c, n := ownerWithNode(t)
	base := time.Now().Add(-7 * time.Minute).Truncate(5 * time.Minute)
	_ = h.Store.Q.InsertSnapshot(t.Context(), db.InsertSnapshotParams{NodeID: n.ID, SessionsLive: 1, MtproxyRaw: []byte("{}")})
	_, _ = h.Store.Pool.Exec(t.Context(), `UPDATE node_stats_snapshots SET taken_at = $1 WHERE node_id = $2 AND taken_at > $1`, base, n.ID)
	q := "from=" + base.Format(time.RFC3339) + "&to=" + time.Now().Format(time.RFC3339)
	var overview struct {
		Series map[string][]struct {
			DcLatency map[string]float64 `json:"dc_latency"`
		} `json:"series"`
	}
	c.JSON(c.Get("/api/v1/monitoring/overview?"+q+"&step=300"), &overview)
	pts := overview.Series[n.ID.String()]
	if len(pts) != 1 || pts[0].DcLatency == nil || len(pts[0].DcLatency) != 0 {
		t.Fatalf("overview bucket without DC data %+v", pts)
	}
}
