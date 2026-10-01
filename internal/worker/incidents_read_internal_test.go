package worker

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

func incidentAlerts(t *testing.T, st *store.Store) (open, unread []int64) {
	t.Helper()
	ctx := context.Background()
	all, err := st.Q.ListOpenAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		open = append(open, a.ID)
	}
	shown, err := st.Q.ListUnreadAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range shown {
		unread = append(unread, a.ID)
	}
	return open, unread
}

func TestReadIncidentStaysReadUntilItClearsAndComesBack(t *testing.T) {
	st := store.OpenTest(t)
	ctx := context.Background()
	var id uuid.UUID
	if err := st.Pool.QueryRow(ctx, `INSERT INTO nodes (name, hostname) VALUES ('n', 'incident.test') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	s := NewStats(st, nil, 90*time.Second, slog.New(slog.DiscardHandler))
	node := db.Node{ID: id}
	sweep := func(failed bool) {
		s.recordFindings(ctx, node, []reliability.Finding{{Kind: "disk_pressure", Message: "disk", Known: true, Failed: failed}})
		s.WaitNotifications()
	}

	sweep(true)
	_, unread := incidentAlerts(t, st)
	if len(unread) != 1 {
		t.Fatalf("want one unread incident, got %d", len(unread))
	}
	if _, err := st.Q.MarkAlertsRead(ctx, unread); err != nil {
		t.Fatal(err)
	}

	sweep(true)
	sweep(true)
	open, unread := incidentAlerts(t, st)
	if len(open) != 1 || len(unread) != 0 {
		t.Fatalf("the next sweeps brought a read incident back: %d open, %d unread", len(open), len(unread))
	}

	sweep(false)
	if open, _ := incidentAlerts(t, st); len(open) != 0 {
		t.Fatalf("a cleared incident stayed open: %v", open)
	}

	sweep(true)
	if _, unread := incidentAlerts(t, st); len(unread) != 1 {
		t.Fatalf("the incident came back but is not shown: %d unread", len(unread))
	}
}
