package worker

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/store/db"
)

func TestHandshakeWindowUsesAcceptedConnectionsAndDoesNotLearnFailures(t *testing.T) {
	var s handshakeSeries
	now := time.Now()
	for i := 0; i < 20; i++ {
		v := s.observe(handshakePoint{at: now.Add(time.Duration(i) * time.Minute), cuts: float64(i * 20), connections: float64(i * 30)})
		if i >= 3 && (!v.known || !v.failed) {
			t.Fatalf("sustained 2/3 interruption share at step %d: %+v", i, v)
		}
	}
}

func TestHandshakeWindowGapAndResetAreUnknown(t *testing.T) {
	for _, gap := range []bool{false, true} {
		var s handshakeSeries
		now := time.Now()
		for i := 0; i < 5; i++ {
			s.observe(handshakePoint{at: now.Add(time.Duration(i) * time.Minute), cuts: float64(i * 20), connections: float64(i * 30)})
		}
		p := handshakePoint{at: now.Add(5 * time.Minute), cuts: 1, connections: 1}
		if gap {
			p = handshakePoint{at: now.Add(8 * time.Minute), cuts: 160, connections: 240}
		}
		if v := s.observe(p); v.known {
			t.Fatalf("gap=%v must be unknown: %+v", gap, v)
		}
	}
}

func TestHandshakePointRejectsUnavailableAndInvalidCounters(t *testing.T) {
	for _, text := range []string{
		"# TYPE telemt_connections_total counter\n# TYPE telemt_handshake_failures_by_class_total counter\n",
		"telemt_connections_total 10\n# TYPE telemt_handshake_failures_by_class_total counter\ntelemt_handshake_failures_by_class_total{class=broken} 2\n",
		"telemt_connections_total NaN\ntelemt_handshake_failures_by_class_total{class=\"timeout\"} 10\n",
		"telemt_connections_total 10\ntelemt_handshake_failures_by_class_total{class=\"timeout\"} +Inf\n",
		"telemt_connections_total 10\ntelemt_handshake_failures_by_class_total{class=\"timeout\"} -1\n",
		"telemt_telemetry_core_enabled 0\ntelemt_connections_total 10\ntelemt_handshake_failures_by_class_total{class=\"timeout\"} 1\n",
	} {
		if _, ok := handshakePointFrom(text, time.Now()); ok {
			t.Fatalf("invalid reading accepted: %q", text)
		}
	}
}

func TestHandshakePointCountsSupportedInterruptionClasses(t *testing.T) {
	text := "telemt_connections_total 10\n" +
		"telemt_handshake_failures_by_class_total{class=\"bad_padding\"} 500\n" +
		"telemt_handshake_failures_by_class_total{class=\"timeout\"} 2\n" +
		"telemt_handshake_failures_by_class_total{class=\"expected_64_got_0_connection_reset\"} 3\n" +
		"telemt_handshake_failures_by_class_total{class=\"other_got_0_noise\"} 100\n"
	p, ok := handshakePointFrom(text, time.Now())
	if !ok || p.cuts != 5 || p.connections != 10 {
		t.Fatalf("point=%+v ok=%v", p, ok)
	}
}

func TestHandshakeWatchPrunesNodesAndBoundsPoints(t *testing.T) {
	var w handshakeWatch
	active, removed := uuid.New(), uuid.New()
	now := time.Now()
	for i := 0; i < 10000; i++ {
		w.observe(active, handshakePoint{at: now.Add(time.Duration(i) * 5 * time.Second), cuts: float64(i), connections: float64(i * 2)})
	}
	w.observe(removed, handshakePoint{at: now})
	w.prune([]db.Node{{ID: active, Status: db.NodeStatusOnline, Engine: db.NodeEngineTelemt}})
	if len(w.nodes) != 1 || len(w.nodes[active].points) > blockingMaxPoints {
		t.Fatalf("history leaked: nodes=%d points=%d", len(w.nodes), len(w.nodes[active].points))
	}
}

func TestHandshakeWindowRejectsClockRegression(t *testing.T) {
	var s handshakeSeries
	now := time.Now()
	for i := 0; i < 5; i++ {
		s.observe(handshakePoint{at: now.Add(time.Duration(i) * time.Minute), cuts: float64(i * 20), connections: float64(i * 30)})
	}
	if v := s.observe(handshakePoint{at: now.Add(3 * time.Minute), cuts: 100, connections: 150}); v.known {
		t.Fatalf("clock regression produced a verdict: %+v", v)
	}
}
