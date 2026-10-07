package worker

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"context"

	"github.com/google/uuid"

	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/telemt"
)

const (
	// blockingMinSpan is the shortest interval a rate is judged over.
	blockingMinSpan = 3 * time.Minute
	// blockingMaxSpan drops samples older than this, so a panel outage is not one long rate.
	blockingMaxSpan = 12 * time.Minute
	// blockingLookback is the interval the comparison aims for.
	blockingLookback = 5 * time.Minute
	// blockingMinCuts is the smallest growth that can be a block rather than background noise.
	blockingMinCuts = 40
	// blockingShare is how much of the new attempts (cuts plus connections) must be cuts.
	blockingShare = 0.5
	// blockingSurge is how many times the quiet rate a jump must be.
	blockingSurge = 4
	// blockingExtremeCuts is a surge large enough to report before a quiet rate is known.
	blockingExtremeCuts  = 120
	blockingExtremeShare = 0.8
	// blockingMinConnRate is the quiet connection rate, per minute, that shows the server
	// normally accepts people. Below it, only an extreme surge is reported.
	blockingMinConnRate = 0.2
)

// handshakeCut is a failure class where the peer never finished saying anything: a timeout, or a
// connection that closed before the first bytes. Protocol errors such as bad padding are a
// different problem and stay with the tls_front_errors check.
func handshakeCut(class string) bool {
	return class == "timeout" || strings.Contains(class, "got_0")
}

type handshakePoint struct {
	at          time.Time
	cuts        float64
	connections float64
}

type handshakeSeries struct {
	points   []handshakePoint
	cutRate  float64
	connRate float64
	ready    bool
}

type blockingVerdict struct {
	known   bool
	failed  bool
	cuts    float64
	conns   float64
	minutes float64
}

func (w *handshakeSeries) observe(p handshakePoint) blockingVerdict {
	if n := len(w.points); n > 0 {
		last := w.points[n-1]
		if p.cuts < last.cuts || p.connections < last.connections {
			*w = handshakeSeries{}
		}
	}
	w.points = append(w.points, p)
	cutoff := p.at.Add(-blockingMaxSpan)
	for len(w.points) > 1 && w.points[0].at.Before(cutoff) {
		w.points = w.points[1:]
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
		if base == -1 || dist < best {
			base, best = i, dist
		}
	}
	if base == -1 {
		return blockingVerdict{}
	}
	older := w.points[base]
	minutes := p.at.Sub(older.at).Minutes()
	if minutes <= 0 {
		return blockingVerdict{}
	}
	dCuts := p.cuts - older.cuts
	dConn := p.connections - older.connections
	rate := dCuts / minutes
	connNow := dConn / minutes
	share := 0.0
	if total := dCuts + dConn; total > 0 {
		share = dCuts / total
	}
	v := blockingVerdict{known: true, cuts: dCuts, conns: dConn, minutes: minutes}
	surge := w.ready && dCuts >= blockingMinCuts && share >= blockingShare &&
		rate >= blockingSurge*w.cutRate && (w.connRate >= blockingMinConnRate || dCuts >= blockingExtremeCuts)
	extreme := !w.ready && dCuts >= blockingExtremeCuts && share >= blockingExtremeShare
	v.failed = surge || extreme
	if !v.failed {
		if !w.ready {
			w.cutRate, w.connRate, w.ready = rate, connNow, true
		} else if share < blockingShare {
			w.cutRate = w.cutRate*0.8 + rate*0.2
			w.connRate = w.connRate*0.8 + connNow*0.2
		}
	}
	return v
}

// handshakeWatch keeps one series per node. Stats sweeps read nodes in parallel.
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

func handshakePointFrom(text string, at time.Time) (handshakePoint, bool) {
	m := telemt.ParseWebMetrics(text)
	if !m.HandshakeFailures.Present || !m.Connections.Present {
		return handshakePoint{}, false
	}
	var cuts float64
	for _, class := range m.HandshakeFailures.Labels() {
		if !handshakeCut(class) {
			continue
		}
		v, _ := m.HandshakeFailures.Get(class)
		cuts += v
	}
	return handshakePoint{at: at, cuts: cuts, connections: m.Connections.Total()}, true
}

func (s *Stats) observeBlocking(ctx context.Context, n db.Node, text string) {
	p, ok := handshakePointFrom(text, s.clock())
	if !ok {
		return
	}
	v := s.handshakes.observe(n.ID, p)
	if !v.known {
		return
	}
	s.recordFindings(ctx, n, []reliability.Finding{{
		Kind: "looks_like_blocking", Message: blockingMessage(v), Failed: v.failed, Known: true,
	}})
}

func blockingMessage(v blockingVerdict) string {
	if !v.failed {
		return "TLS handshakes are completing again"
	}
	return fmt.Sprintf("Most new connections are cut during the TLS handshake (%.0f cut, %.0f connected over %.0f min). This is what blocking the server looks like.",
		v.cuts, v.conns, v.minutes)
}
