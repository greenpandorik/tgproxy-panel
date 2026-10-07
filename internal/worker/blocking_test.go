package worker_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

func TestBlockingAlertOpensOnAHandshakeSurgeAndClosesWhenItPasses(t *testing.T) {
	f := newTelemtFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	if err := f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID}); err != nil {
		t.Fatal(err)
	}
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	s.SetNow(func() time.Time { return now })

	cuts, padding, conns := 0, 0, 100
	sweep := func(after time.Duration) {
		t.Helper()
		now = now.Add(after)
		mock.SetMetrics(f.node.ID, fmt.Sprintf(
			"telemt_connections_total %d\n"+
				"telemt_handshake_failures_by_class_total{class=%q} %d\n"+
				"telemt_handshake_failures_by_class_total{class=%q} %d\n",
			conns, "timeout", cuts, "bad_padding", padding))
		if err := s.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	open := func() int {
		t.Helper()
		alerts, err := f.st.Q.ListOpenAlerts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, a := range alerts {
			if a.Kind == "reliability_looks_like_blocking" && a.NodeID.UUID == f.node.ID {
				n++
			}
		}
		return n
	}

	sweep(0)
	padding, cuts, conns = 400, 2, 120
	sweep(4 * time.Minute)
	padding, cuts, conns = 900, 4, 140
	sweep(4 * time.Minute)
	if open() != 0 {
		t.Fatal("protocol errors and a quiet rate must not raise the alert")
	}

	cuts, conns = 84, 142
	sweep(4 * time.Minute)
	cuts, conns = 110, 143
	sweep(time.Minute)
	if open() != 0 {
		t.Fatal("the alert waits for three sweeps, like the other per-minute problems")
	}
	cuts, conns = 140, 144
	sweep(time.Minute)
	if n := open(); n != 1 {
		t.Fatalf("open alerts: %d", n)
	}

	for range 3 {
		conns += 20
		sweep(5 * time.Minute)
	}
	if open() != 0 {
		t.Fatal("the alert stayed open after handshakes completed again")
	}
}
