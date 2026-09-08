package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tgwebproxy/internal/store/db"
)

// maxSeriesPoints bounds how many points a single /monitoring/nodes/{id}/series
// response returns; maxOverviewPoints does the same per node for the overview
// endpoint's rate series.
const (
	maxSeriesPoints   = 2000
	maxOverviewPoints = 600
)

// maxRangeSpan caps the window a monitoring request may ask for. Snapshots are pruned at 30
// days (worker.Stats.RunOnce), so nothing beyond this exists to plot; without the cap a
// hand-edited from=1970-01-01 makes the server read every retained row for every node into
// memory, and any authenticated role - viewers included - can poll that endpoint.
const maxRangeSpan = 31 * 24 * time.Hour

// bucketStepThreshold is the step above which the overview aggregates in SQL instead of
// thinning in Go. At or below it the raw one-per-minute rows are already at (or finer than)
// the requested resolution, so bucketing would buy nothing.
const bucketStepThreshold = 60

func (s *Server) mountMonitoring(r chi.Router) {
	r.Get("/monitoring/overview", s.handleMonitoringOverview)
	r.Get("/monitoring/nodes/{id}/series", s.handleMonitoringSeries)
}

// parseRFC3339Query parses a query value as RFC3339. A literal "+" in a
// non-UTC offset (e.g. "+03:00") arrives here decoded to a space - form
// query decoding treats "+" as an escaped space - so a bare space where the
// offset sign belongs is restored before parsing; RFC3339 never contains a
// real space.
func parseRFC3339Query(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, strings.ReplaceAll(v, " ", "+"))
}

// parseFromTo reads the "from"/"to" RFC3339 query params, defaulting to
// [now-24h, now]. It writes a 400 response and returns ok=false on a parse
// error.
func parseFromTo(w http.ResponseWriter, r *http.Request) (from, to time.Time, ok bool) {
	q := r.URL.Query()
	to = time.Now()
	if v := q.Get("to"); v != "" {
		t, err := parseRFC3339Query(v)
		if err != nil {
			badRequest(w, "invalid to")
			return time.Time{}, time.Time{}, false
		}
		to = t
	}
	from = to.Add(-24 * time.Hour)
	if v := q.Get("from"); v != "" {
		t, err := parseRFC3339Query(v)
		if err != nil {
			badRequest(w, "invalid from")
			return time.Time{}, time.Time{}, false
		}
		from = t
	}
	if from.After(to) {
		badRequest(w, "from must not be after to")
		return time.Time{}, time.Time{}, false
	}
	if to.Sub(from) > maxRangeSpan {
		badRequest(w, "range too wide: at most 31 days (snapshots are retained for 30)")
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

func (s *Server) handleMonitoringSeries(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	from, to, ok := parseFromTo(w, r)
	if !ok {
		return
	}
	rows, err := s.store.Q.ListSnapshots(r.Context(), db.ListSnapshotsParams{NodeID: n.ID, TakenAt: from, TakenAt_2: to})
	if err != nil {
		internal(w)
		return
	}
	rows = capSamples(rows, maxSeriesPoints)
	points := make([]map[string]any, 0, len(rows))
	for _, snap := range rows {
		points = append(points, map[string]any{
			"t": snap.TakenAt, "sessions_live": snap.SessionsLive, "streams_live": snap.StreamsLive,
			"bytes_up": snap.BytesUp, "bytes_down": snap.BytesDown,
			"cpu_percent": snap.CpuPercent, "mem_used_percent": snap.MemUsedPercent, "disk_used_percent": snap.DiskUsedPercent,
			"dc_latency": dcLatencyRaw(snap.DcLatency),
		})
	}
	writeJSON(w, 200, map[string]any{"points": points})
}

// dcLatencyRaw passes a snapshot's dc_latency object through as stored. The column is NOT NULL
// DEFAULT '{}', so an empty value can only come from a row read before the column existed in
// the caller's view; it is emitted as an empty object so the client never sees null.
func dcLatencyRaw(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(b)
}

type monitoringNodeJSON struct {
	NodeID   uuid.UUID `json:"node_id"`
	NodeName string    `json:"node_name"`
	Hostname string    `json:"hostname"`
	Status   string    `json:"status"`
}

type monitoringPointJSON struct {
	T             time.Time `json:"t"`
	SessionsLive  int32     `json:"sessions_live"`
	StreamsLive   int32     `json:"streams_live"`
	BytesUpRate   float64   `json:"bytes_up_rate"`
	BytesDownRate float64   `json:"bytes_down_rate"`
	// Server load, averaged over the bucket like the gauges above.
	CPUPercent      float32 `json:"cpu_percent"`
	MemUsedPercent  float32 `json:"mem_used_percent"`
	DiskUsedPercent float32 `json:"disk_used_percent"`
	// DcLatency is {"<dc>": <ms>}, per DC the mean over the bucket's rows that measured it.
	DcLatency json.RawMessage `json:"dc_latency"`
}

// overviewSample is one stats sample feeding the rate series, whether it came from a raw
// snapshot row or from a SQL time bucket. Having both paths converge here keeps the rate
// computation - the part with the counter-reset and first-point rules - in exactly one place.
type overviewSample struct {
	NodeID       uuid.UUID
	T            time.Time
	SessionsLive int32
	StreamsLive  int32
	BytesUp      int64
	BytesDown    int64

	CPUPercent, MemUsedPercent, DiskUsedPercent float32
	DcLatency                                   []byte
}

// overviewSamples reads the snapshots backing the overview. Above bucketStepThreshold it
// aggregates in SQL, so the rows crossing the wire are bounded by the requested resolution
// rather than by retention; at or below it the raw rows already are the resolution.
func (s *Server) overviewSamples(r *http.Request, from, to time.Time, stepSeconds int) ([]overviewSample, error) {
	if stepSeconds > bucketStepThreshold {
		rows, err := s.store.Q.ListSnapshotsAllNodesBucketed(r.Context(), db.ListSnapshotsAllNodesBucketedParams{
			Step: int64(stepSeconds), FromAt: from, ToAt: to,
		})
		if err != nil {
			return nil, err
		}
		out := make([]overviewSample, 0, len(rows))
		for _, row := range rows {
			out = append(out, overviewSample{
				NodeID: row.NodeID, T: row.TakenAt, SessionsLive: row.SessionsLive,
				StreamsLive: row.StreamsLive, BytesUp: row.BytesUp, BytesDown: row.BytesDown,
				CPUPercent: row.CpuPercent, MemUsedPercent: row.MemUsedPercent, DiskUsedPercent: row.DiskUsedPercent,
				DcLatency: row.DcLatency,
			})
		}
		return out, nil
	}
	rows, err := s.store.Q.ListSnapshotsAllNodes(r.Context(), db.ListSnapshotsAllNodesParams{TakenAt: from, TakenAt_2: to})
	if err != nil {
		return nil, err
	}
	out := make([]overviewSample, 0, len(rows))
	for _, row := range rows {
		out = append(out, overviewSample{
			NodeID: row.NodeID, T: row.TakenAt, SessionsLive: row.SessionsLive,
			StreamsLive: row.StreamsLive, BytesUp: row.BytesUp, BytesDown: row.BytesDown,
			CPUPercent: row.CpuPercent, MemUsedPercent: row.MemUsedPercent, DiskUsedPercent: row.DiskUsedPercent,
			DcLatency: row.DcLatency,
		})
	}
	return out, nil
}

// handleMonitoringOverview returns every node plus, per node, a rate series
// derived from consecutive stats-snapshot deltas: rate = max(0, cur-prev) /
// seconds(cur-prev), clamped to 0 on a counter reset (relay restart) and for
// each node's first point. Series are sampled to at most maxOverviewPoints
// per node; an optional "step" (seconds) query param sets the resolution -
// above bucketStepThreshold the samples are averaged into step-wide buckets by
// the database (so a wide range no longer plots one arbitrary 60s rate every
// half hour), below it the raw rows are thinned in Go to that spacing.
func (s *Server) handleMonitoringOverview(w http.ResponseWriter, r *http.Request) {
	from, to, ok := parseFromTo(w, r)
	if !ok {
		return
	}
	stepSeconds := 0
	if v := r.URL.Query().Get("step"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			badRequest(w, "invalid step")
			return
		}
		stepSeconds = n
	}

	nodeRows, err := s.store.Q.ListNodes(r.Context())
	if err != nil {
		internal(w)
		return
	}
	nodes := make([]monitoringNodeJSON, 0, len(nodeRows))
	for _, n := range nodeRows {
		nodes = append(nodes, monitoringNodeJSON{NodeID: n.ID, NodeName: n.Name, Hostname: n.Hostname, Status: string(n.Status)})
	}

	snapRows, err := s.overviewSamples(r, from, to, stepSeconds)
	if err != nil {
		internal(w)
		return
	}

	// snapRows is ordered node_id, time (per the query), so grouping by
	// appending to the current node's slice preserves per-node time order.
	byNode := make(map[uuid.UUID][]overviewSample)
	order := make([]uuid.UUID, 0, len(nodes))
	for _, row := range snapRows {
		if _, seen := byNode[row.NodeID]; !seen {
			order = append(order, row.NodeID)
		}
		byNode[row.NodeID] = append(byNode[row.NodeID], row)
	}

	series := make(map[string][]monitoringPointJSON, len(order))
	for _, nodeID := range order {
		rows := byNode[nodeID]
		points := make([]monitoringPointJSON, 0, len(rows))
		for i, row := range rows {
			p := monitoringPointJSON{
				T: row.T, SessionsLive: row.SessionsLive, StreamsLive: row.StreamsLive,
				CPUPercent: row.CPUPercent, MemUsedPercent: row.MemUsedPercent, DiskUsedPercent: row.DiskUsedPercent,
				DcLatency: dcLatencyRaw(row.DcLatency),
			}
			if i > 0 {
				prev := rows[i-1]
				seconds := row.T.Sub(prev.T).Seconds()
				if seconds > 0 {
					p.BytesUpRate = rate(prev.BytesUp, row.BytesUp, seconds)
					p.BytesDownRate = rate(prev.BytesDown, row.BytesDown, seconds)
				}
			}
			points = append(points, p)
		}
		series[nodeID.String()] = sampleOverviewPoints(points, stepSeconds, maxOverviewPoints)
	}

	writeJSON(w, 200, map[string]any{"nodes": nodes, "series": series})
}

// rate computes a clamped-at-zero bytes/second rate from a counter delta;
// a negative delta (relay restart resets the counter) yields 0 rather than
// a bogus negative rate.
func rate(prev, cur int64, seconds float64) float64 {
	delta := cur - prev
	if delta < 0 {
		return 0
	}
	return float64(delta) / seconds
}

// sampleOverviewPoints first thins points to be at least stepSeconds apart
// (when stepSeconds > 0), then caps the result to maxPoints via capSamples.
// Above bucketStepThreshold the database has already produced exactly one point per
// step-wide bucket, so the thinning pass keeps every point and only the cap can bite.
func sampleOverviewPoints(points []monitoringPointJSON, stepSeconds, maxPoints int) []monitoringPointJSON {
	if stepSeconds > 0 && len(points) > 0 {
		thinned := make([]monitoringPointJSON, 0, len(points))
		var last time.Time
		for i, p := range points {
			if i == 0 || p.T.Sub(last) >= time.Duration(stepSeconds)*time.Second {
				thinned = append(thinned, p)
				last = p.T
			}
		}
		points = thinned
	}
	return capSamples(points, maxPoints)
}

// capSamples downsamples items to at most maxPoints by taking an even
// stride through the slice, starting at index 0 (so the first item is
// always kept). A plain stride loop can land short of the last index (e.g.
// 1000 items strided by 2 stops at index 998, dropping the most recent
// point) - a chart caller cares about that last point most of all, so it is
// always included too, swapped in for the last sampled item rather than
// appended when the loop already produced maxPoints entries.
func capSamples[T any](items []T, maxPoints int) []T {
	n := len(items)
	if n <= maxPoints || maxPoints <= 0 {
		return items
	}
	stride := (n + maxPoints - 1) / maxPoints
	out := make([]T, 0, maxPoints)
	lastIdx := -1
	for i := 0; i < n; i += stride {
		out = append(out, items[i])
		lastIdx = i
	}
	if lastIdx != n-1 {
		if len(out) >= maxPoints {
			out[len(out)-1] = items[n-1]
		} else {
			out = append(out, items[n-1])
		}
	}
	return out
}
