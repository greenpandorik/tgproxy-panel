package worker_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

type fleetBlockingDriver struct {
	*nodedriver.Mock
	entered chan uuid.UUID
	release chan struct{}
}

func (d *fleetBlockingDriver) Apply(ctx context.Context, id uuid.UUID, req nodedriver.ApplyRequest) (nodedriver.ApplyResult, error) {
	d.entered <- id
	select {
	case <-d.release:
	case <-ctx.Done():
		return nodedriver.ApplyResult{}, ctx.Err()
	}
	return d.Mock.Apply(ctx, id, req)
}

func TestBackgroundApplyLimitsFleetConcurrencyAndRetainsDeferredNodes(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	d := &fleetBlockingDriver{Mock: nodedriver.NewMock(), entered: make(chan uuid.UUID, 20), release: make(chan struct{})}
	ids := []uuid.UUID{f.node.ID}
	for _, name := range []string{"two", "three", "four", "five", "six"} {
		n, err := f.st.Q.CreateNode(ctx, db.CreateNodeParams{Name: name, Hostname: name + ".test"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	for _, id := range ids {
		d.SetOnline(id, true)
		if err := f.st.Q.SetNodeDirty(ctx, db.SetNodeDirtyParams{ID: id, Dirty: true}); err != nil {
			t.Fatal(err)
		}
	}
	a := worker.NewApply(f.st, f.box, d, 10*time.Millisecond, slog.New(slog.DiscardHandler))
	released := false
	t.Cleanup(func() {
		cancel()
		if !released {
			close(d.release)
		}
		if err := a.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	go a.Run(ctx)
	for range 4 {
		select {
		case <-d.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("four initial applies did not start")
		}
	}
	select {
	case id := <-d.entered:
		t.Fatalf("fifth apply %s started before a slot was released", id)
	case <-time.After(100 * time.Millisecond):
	}
	close(d.release)
	released = true
	for range 2 {
		select {
		case <-d.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("deferred dirty nodes were lost")
		}
	}
}
