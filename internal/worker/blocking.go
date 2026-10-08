package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/telemt"
)

const (
	blockingMinSpan   = 3 * time.Minute
	blockingMaxSpan   = 12 * time.Minute
	blockingLookback  = 5 * time.Minute
	blockingMinCuts   = 40
	blockingShare     = .5
	blockingMaxPoints = 64
)

// These classes describe interrupted handshakes, including plain MTProto and
// errors after bytes have arrived. They do not prove TLS filtering or zero-byte clients.
func handshakeCut(class string) bool {
	switch class {
	case "timeout", "expected_64_got_0_unexpected_eof", "expected_64_got_0_connection_reset", "expected_64_got_0_connection_aborted", "expected_64_got_0_broken_pipe", "expected_64_got_0_not_connected":
		return true
	}
	return false
}

type (
	handshakePoint struct {
		at                time.Time
		cuts, connections float64
		uptime            *float64
	}
	handshakeSeries struct{ points []handshakePoint }
	blockingVerdict struct {
		known, failed        bool
		cuts, conns, minutes float64
	}
)

func (w *handshakeSeries) observe(p handshakePoint) blockingVerdict {
	if n := len(w.points); n > 0 {
		last := w.points[n-1]
		if !p.at.After(last.at) || p.at.Sub(last.at) > reliabilityFreshness || p.cuts < last.cuts || p.connections < last.connections || (p.uptime != nil && last.uptime != nil && *p.uptime < *last.uptime) {
			w.points = nil
		}
	}
	w.points = append(w.points, p)
	cutoff := p.at.Add(-blockingMaxSpan)
	for len(w.points) > 1 && w.points[0].at.Before(cutoff) {
		w.points = w.points[1:]
	}
	if len(w.points) > blockingMaxPoints {
		w.points = append([]handshakePoint(nil), w.points[len(w.points)-blockingMaxPoints:]...)
	}
	base := -1
	best := blockingMaxSpan
	for i := 0; i < len(w.points)-1; i++ {
		age := p.at.Sub(w.points[i].at)
		if age < blockingMinSpan || age > blockingMaxSpan {
			continue
		}
		dist := age - blockingLookback
		if dist < 0 {
			dist = -dist
		}
		if base < 0 || dist < best {
			base, best = i, dist
		}
	}
	if base < 0 {
		return blockingVerdict{}
	}
	old := w.points[base]
	cuts, accepted := p.cuts-old.cuts, p.connections-old.connections
	v := blockingVerdict{known: true, cuts: cuts, conns: accepted, minutes: p.at.Sub(old.at).Minutes()}
	// Accepted connections already include interrupted attempts. Completions can
	// cross window boundaries, so this is only an approximate share, never success accounting.
	v.failed = accepted > 0 && cuts >= blockingMinCuts && cuts/accepted >= blockingShare
	return v
}

type handshakeWatch struct {
	mu    sync.Mutex
	nodes map[uuid.UUID]*handshakeSeries
}

func (w *handshakeWatch) observe(id uuid.UUID, p handshakePoint) blockingVerdict {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.nodes == nil {
		w.nodes = map[uuid.UUID]*handshakeSeries{}
	}
	s := w.nodes[id]
	if s == nil {
		s = &handshakeSeries{}
		w.nodes[id] = s
	}
	return s.observe(p)
}

func (w *handshakeWatch) forget(id uuid.UUID) { w.mu.Lock(); defer w.mu.Unlock(); delete(w.nodes, id) }

func (w *handshakeWatch) prune(nodes []db.Node) {
	active := map[uuid.UUID]bool{}
	for _, n := range nodes {
		if n.Engine == db.NodeEngineTelemt && n.Status != db.NodeStatusOffline && n.Status != db.NodeStatusPending {
			active[n.ID] = true
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for id := range w.nodes {
		if !active[id] {
			delete(w.nodes, id)
		}
	}
}

func validCounter(v float64) bool { return v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
func handshakePointFrom(text string, at time.Time) (handshakePoint, bool) {
	m := telemt.ParseWebMetrics(text)
	accepted, ok := m.Connections.Get("")
	if !ok || !m.HandshakeFailures.Present || !validCounter(accepted) {
		return handshakePoint{}, false
	}
	p := handshakePoint{at: at, connections: accepted}
	for _, class := range m.HandshakeFailures.Labels() {
		if class == "" {
			return handshakePoint{}, false
		}
		v, _ := m.HandshakeFailures.Get(class)
		if !validCounter(v) {
			return handshakePoint{}, false
		}
		if handshakeCut(class) {
			p.cuts += v
		}
	}
	reportedFailures := 0
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if f[0] == telemt.MetricHandshakeFailures || strings.HasPrefix(f[0], telemt.MetricHandshakeFailures+"{") {
			reportedFailures++
			if len(f) < 2 {
				return handshakePoint{}, false
			}
			v, e := strconv.ParseFloat(f[1], 64)
			if e != nil || !validCounter(v) {
				return handshakePoint{}, false
			}
		}
		if f[0] == "telemt_telemetry_core_enabled" || f[0] == "telemt_uptime_seconds" {
			if len(f) < 2 {
				return handshakePoint{}, false
			}
			v, e := strconv.ParseFloat(f[1], 64)
			if e != nil || !validCounter(v) {
				return handshakePoint{}, false
			}
			if f[0] == "telemt_telemetry_core_enabled" && v != 1 {
				return handshakePoint{}, false
			}
			if f[0] == "telemt_uptime_seconds" {
				p.uptime = &v
			}
		}
	}
	return p, validCounter(p.cuts) && reportedFailures == len(m.HandshakeFailures.Values)
}

// blockingProbeEvidence stores only the identities of authenticated checks that
// actually ran. This survives panel restarts; evidence rows are excluded from
// aggregate notification health, so their age cannot create a notification.
func (s *Stats) blockingProbeEvidence(ctx context.Context, n db.Node) (failed, healthy bool) {
	if len(s.probeLocations) == 0 {
		return false, false
	}
	now := s.clock()
	rows, e := s.st.Pool.Query(ctx, `SELECT report FROM probe_reports WHERE node_id=$1`, n.ID)
	if e != nil {
		return false, false
	}
	reports := map[string]reliability.ProbeReport{}
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			rows.Close()
			return false, false
		}
		var p reliability.ProbeReport
		if json.Unmarshal(raw, &p) == nil {
			reports[p.Location] = p
		}
	}
	err := rows.Err()
	rows.Close()
	if err != nil {
		return false, false
	}
	checks := map[string]reliability.ProbeCheck{}
	healthy = true
	for _, loc := range s.probeLocations {
		p, ok := reports[loc]
		if !ok || p.At.Sub(now) > 30*time.Second {
			healthy = false
			continue
		}
		fresh := now.Sub(p.At) <= reliabilityFreshness
		if !fresh {
			healthy = false
		}
		executed := false
		for key, c := range map[string]reliability.ProbeCheck{"faketls": p.FakeTLS, "web": p.WEB} {
			if c.Method != reliability.AuthenticatedMTProto || (c.Status != "ok" && c.Status != "failed") {
				continue
			}
			executed = true
			id := loc + "|" + key
			if fresh {
				checks[id] = c
				failed = failed || c.Status == "failed"
			}
			// Backfill reports accepted before ingestion retained identities. Their
			// age never certifies health, but their executed transport stays required.
			if _, e = s.st.Pool.Exec(ctx, `INSERT INTO notification_findings(node_id,source,kind,failed,known,observed_at) VALUES($1,'blocking_evidence',$2,false,true,$3) ON CONFLICT(node_id,kind) DO UPDATE SET observed_at=GREATEST(notification_findings.observed_at,excluded.observed_at)`, n.ID, id, p.At); e != nil {
				return false, false
			}
		}
		if !executed {
			healthy = false
		}
	}
	rows, e = s.st.Pool.Query(ctx, `SELECT kind FROM notification_findings WHERE node_id=$1 AND source='blocking_evidence'`, n.ID)
	if e != nil {
		return false, false
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return false, false
		}
		c, ok := checks[id]
		if !ok || c.Status != "ok" {
			healthy = false
		}
	}
	if rows.Err() != nil {
		return false, false
	}
	return failed, healthy && !failed
}

func (s *Stats) retireBlockingEvidence(ctx context.Context, n db.Node) {
	sources := make([]string, 0, len(s.probeLocations))
	for _, loc := range s.probeLocations {
		sources = append(sources, "probe_"+loc)
	}
	_, err := s.st.Pool.Exec(ctx, `DELETE FROM notification_findings WHERE node_id=$1 AND ((source='blocking_evidence' AND split_part(kind,'|',1)<>ALL($2::text[])) OR (source='blocking' AND ($3 OR cardinality($2::text[])=0)) OR (source LIKE 'probe\_%' ESCAPE '\' AND source<>ALL($4::text[])))`, n.ID, append([]string{}, s.probeLocations...), n.Engine != db.NodeEngineTelemt, sources)
	if err != nil {
		s.log.Error("retire removed blocking/probe observations", "err", err)
	}
}

func (s *Stats) observeBlocking(ctx context.Context, n db.Node, text string) {
	finding := reliability.Finding{Kind: "looks_like_blocking"}
	// Persist executed check identities even during metric warmup or outages.
	failed, healthy := s.blockingProbeEvidence(ctx, n)
	var h struct{ Reliability reliability.Report }
	if json.Unmarshal(n.LastHealth, &h) == nil && h.Reliability.Policy.Maintenance {
		s.handshakes.forget(n.ID)
		s.recordFindings(ctx, n, []reliability.Finding{finding})
		return
	}
	if len(s.probeLocations) > 0 {
		p, ok := handshakePointFrom(text, s.clock())
		if ok {
			v := s.handshakes.observe(n.ID, p)
			if v.known {
				finding.Known = healthy || (v.failed && failed)
				finding.Failed = v.failed && failed
				v.failed = finding.Failed
				finding.Message = blockingMessage(v)
			}
		} else {
			s.handshakes.forget(n.ID)
		}
	} else {
		s.handshakes.forget(n.ID)
	}
	s.recordFindings(ctx, n, []reliability.Finding{finding})
}

func blockingMessage(v blockingVerdict) string {
	if !v.failed {
		return "Authenticated proxy checks are working again"
	}
	return fmt.Sprintf("%.0f interrupted handshakes among %.0f accepted connections over %.0f min, with an authenticated external proxy check failing. Possible network filtering or a server/configuration problem.", v.cuts, v.conns, v.minutes)
}
