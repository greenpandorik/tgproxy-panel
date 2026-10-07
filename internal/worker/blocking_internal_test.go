package worker

import (
	"testing"
	"time"
)

func TestHandshakeWatchLearnsQuietTrafficThenFlagsASurge(t *testing.T) {
	var w handshakeSeries
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration, cuts, conns float64) blockingVerdict {
		t.Helper()
		return w.observe(handshakePoint{at: base.Add(d), cuts: cuts, connections: conns})
	}
	if v := at(0, 0, 100); v.known {
		t.Fatal("one sample is not a rate")
	}
	if v := at(4*time.Minute, 4, 120); !v.known || v.failed {
		t.Fatalf("a quiet window only teaches the baseline: %+v", v)
	}
	if v := at(8*time.Minute, 8, 140); v.failed {
		t.Fatalf("the same rate is not a block: %+v", v)
	}
	if v := at(12*time.Minute, 88, 142); !v.failed || v.cuts < 80 || v.conns > 5 {
		t.Fatalf("surge: %+v", v)
	}
}

func TestHandshakeWatchIgnoresScannersOnAnIdleServer(t *testing.T) {
	var w handshakeSeries
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if v := w.observe(handshakePoint{at: base, cuts: 0, connections: 0}); v.known {
		t.Fatal("one sample is not a rate")
	}
	if v := w.observe(handshakePoint{at: base.Add(4 * time.Minute), cuts: 30, connections: 0}); !v.known || v.failed {
		t.Fatalf("the first scanner window becomes the baseline: %+v", v)
	}
	for i := 2; i <= 6; i++ {
		v := w.observe(handshakePoint{at: base.Add(time.Duration(i) * 4 * time.Minute), cuts: float64(i * 30), connections: 0})
		if v.failed {
			t.Fatalf("steady scanners at step %d: %+v", i, v)
		}
	}
}

func TestHandshakeWatchFlagsAFloodBeforeItHasABaseline(t *testing.T) {
	var w handshakeSeries
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	_ = w.observe(handshakePoint{at: base, cuts: 0, connections: 10})
	v := w.observe(handshakePoint{at: base.Add(4 * time.Minute), cuts: 200, connections: 10})
	if !v.failed || v.cuts != 200 {
		t.Fatalf("a flood on the first window: %+v", v)
	}
	if w.ready {
		t.Fatal("a flood must not become the quiet rate")
	}
}

func TestHandshakeWatchTreatsACounterResetAsANewSeries(t *testing.T) {
	var w handshakeSeries
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	_ = w.observe(handshakePoint{at: base, cuts: 500, connections: 900})
	_ = w.observe(handshakePoint{at: base.Add(4 * time.Minute), cuts: 510, connections: 940})
	v := w.observe(handshakePoint{at: base.Add(8 * time.Minute), cuts: 3, connections: 1})
	if v.known || v.failed || w.ready || len(w.points) != 1 {
		t.Fatalf("a restart must drop the history: %+v points=%d ready=%v", v, len(w.points), w.ready)
	}
}

func TestHandshakePointCountsOnlyCutClasses(t *testing.T) {
	text := "telemt_connections_total 10\n" +
		"telemt_handshake_failures_by_class_total{class=\"bad_padding\"} 500\n" +
		"telemt_handshake_failures_by_class_total{class=\"timeout\"} 2\n" +
		"telemt_handshake_failures_by_class_total{class=\"expected_64_got_0_connection_reset\"} 3\n"
	p, ok := handshakePointFrom(text, time.Unix(0, 0))
	if !ok || p.cuts != 5 || p.connections != 10 {
		t.Fatalf("point %+v ok=%v", p, ok)
	}
	if _, ok := handshakePointFrom("telemt_connections_total 1\n", time.Time{}); ok {
		t.Fatal("a scrape without the handshake family is not a reading")
	}
}
