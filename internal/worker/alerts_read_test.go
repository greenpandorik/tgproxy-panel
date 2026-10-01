package worker_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/worker"
)

func countAlerts(t *testing.T, st *store.Store, kind string) (open, unread int) {
	t.Helper()
	ctx := context.Background()
	all, err := st.Q.ListOpenAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		if a.Kind == kind {
			open++
		}
	}
	shown, err := st.Q.ListUnreadAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range shown {
		if a.Kind == kind {
			unread++
		}
	}
	return open, unread
}

func TestReadApplyFailureStaysReadWhileRetriesKeepFailing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	if _, err := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}}); err != nil {
		t.Fatal(err)
	}
	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	fail := func() {
		t.Helper()
		mock.FailNextApply(f.node.ID, "relay -check rejected profiles")
		if err := a.ApplyNode(ctx, f.node.ID); err == nil {
			t.Fatal("expected the apply to fail")
		}
	}

	fail()
	fail()
	if open, unread := countAlerts(t, f.st, "apply_failed"); open != 1 || unread != 1 {
		t.Fatalf("retries raised %d open / %d unread alerts, want one", open, unread)
	}
	shown, _ := f.st.Q.ListUnreadAlerts(ctx)
	if _, err := f.st.Q.MarkAlertsRead(ctx, []int64{shown[0].ID}); err != nil {
		t.Fatal(err)
	}

	fail()
	if open, unread := countAlerts(t, f.st, "apply_failed"); open != 1 || unread != 0 {
		t.Fatalf("a retry brought the read alert back: %d open / %d unread", open, unread)
	}

	if err := a.ApplyNode(ctx, f.node.ID); err != nil {
		t.Fatal(err)
	}
	if open, _ := countAlerts(t, f.st, "apply_failed"); open != 0 {
		t.Fatalf("a successful apply left %d alerts open", open)
	}
	fail()
	if _, unread := countAlerts(t, f.st, "apply_failed"); unread != 1 {
		t.Fatalf("a new failure after a success must show again, got %d unread", unread)
	}
}
