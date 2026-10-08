package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"tgwebproxy/internal/alerttext"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/notify"
	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

type queuedSender struct{ calls atomic.Int32 }

func (s *queuedSender) SendWith(context.Context, string, string, string) error {
	s.calls.Add(1)
	return nil
}

func TestReliabilityDebounceSurvivesFullNotificationQueue(t *testing.T) {
	st := store.OpenTest(t)
	var id uuid.UUID
	if err := st.Pool.QueryRow(t.Context(), `INSERT INTO nodes(name,hostname) VALUES('queue','queue.test') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	driver := nodedriver.NewMock()
	driver.SetOnline(id, true)
	sender := &queuedSender{}
	s := NewStats(st, driver, time.Hour, slog.New(slog.DiscardHandler))
	s.SetAlerts(NewAlerts(func(context.Context) (bool, string, string, error) { return true, "tok", "42", nil }, sender, slog.New(slog.DiscardHandler)))
	now := time.Now()
	s.SetNow(func() time.Time { return now })
	release := make(chan struct{})
	for range maxInFlightNotifications {
		s.notify(t.Context(), func(context.Context) { <-release })
	}
	sweep := func() {
		raw, _ := json.Marshal(reliability.Report{Version: 1, At: now, DCs: []reliability.DC{{ID: 1, Alive: 0, Required: 1}}})
		health, _ := json.Marshal(nodedriver.HealthReport{Readyz: true, Healthz: true, Reliability: raw})
		if err := st.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{ID: id, Status: db.NodeStatusOnline, LastHealth: health}); err != nil {
			t.Fatal(err)
		}
		if err := s.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	for range 6 {
		sweep()
		now = now.Add(time.Minute)
	}
	close(release)
	s.WaitNotifications()
	sweep()
	s.WaitNotifications()
	if got := sender.calls.Load(); got != 1 {
		t.Fatalf("full queue lost durable problem debounce: sends=%d, want 1", got)
	}
}

func TestNeverConfiguredChecksDoNotPreventStableRecovery(t *testing.T) {
	st := store.OpenTest(t)
	var id uuid.UUID
	if err := st.Pool.QueryRow(t.Context(), `INSERT INTO nodes(name,hostname) VALUES('optional','optional.test') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	sender := &queuedSender{}
	a := NewAlerts(func(context.Context) (bool, string, string, error) { return true, "tok", "42", nil }, sender, slog.New(slog.DiscardHandler))
	s := NewStats(st, nil, time.Hour, slog.New(slog.DiscardHandler))
	s.SetAlerts(a)
	now := time.Now()
	s.SetNow(func() time.Time { return now })
	n := db.Node{ID: id, Name: "optional"}
	tick := func(failed, known bool) {
		s.recordFindings(t.Context(), n, []reliability.Finding{{Kind: "disk_pressure", Known: true, Failed: failed}, {Kind: "never_configured", Known: false}})
		if !a.observeReliability(t.Context(), n, known, now) {
			t.Fatal("observation failed")
		}
		a.sendReliability(t.Context(), n)
		now = now.Add(time.Minute)
	}
	for range 6 {
		tick(true, true)
	}
	if sender.calls.Load() != 1 {
		t.Fatal("persistent problem was not sent")
	}
	for range 8 {
		tick(false, true)
	}
	tick(false, false)
	for range 10 {
		tick(false, true)
	}
	if sender.calls.Load() != 1 {
		t.Fatal("unknown interval counted toward recovery")
	}
	tick(false, true)
	if sender.calls.Load() != 2 {
		t.Fatal("unconfigured check prevented verified recovery")
	}
}

func TestScheduledDCIncidentsJoinOneServerEpisode(t *testing.T) {
	st := store.OpenTest(t)
	var id uuid.UUID
	if err := st.Pool.QueryRow(t.Context(), `INSERT INTO nodes(name,hostname) VALUES('diagnostics','diagnostics.test') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	sender := &queuedSender{}
	a := NewAlerts(func(context.Context) (bool, string, string, error) { return true, "tok", "42", nil }, sender, slog.New(slog.DiscardHandler))
	s := NewStats(st, nil, time.Hour, slog.New(slog.DiscardHandler))
	s.SetAlerts(a)
	n := db.Node{ID: id, Name: "diagnostics"}
	now := time.Now()
	s.SetNow(func() time.Time { return now })
	for _, dc := range []string{"1", "2", "3", "4", "5", "203", "-203"} {
		kind := "diagnostic_telegram_dc_writers_" + dc
		if _, err := st.Pool.Exec(t.Context(), `INSERT INTO alerts(node_id,kind,message) VALUES($1,$2,'DC warning')`, id, kind); err != nil {
			t.Fatal(err)
		}
		a.Incident(t.Context(), n, alerttext.Incident{Kind: kind})
	}
	if sender.calls.Load() != 0 {
		t.Fatal("scheduled DC transitions sent individual messages")
	}
	tick := func(known bool) {
		s.recordFindings(t.Context(), n, []reliability.Finding{{Kind: "disk_pressure", Known: true}})
		if !a.observeReliability(t.Context(), n, known, now) {
			t.Fatal("observation failed")
		}
		a.sendReliability(t.Context(), n)
		now = now.Add(time.Minute)
	}
	for range 6 {
		tick(true)
	}
	if sender.calls.Load() != 1 {
		t.Fatal("simultaneous diagnostic incidents did not produce one summary")
	}
	if _, err := st.Pool.Exec(t.Context(), `UPDATE alerts SET resolved_at=now() WHERE node_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	for range 9 {
		tick(true)
	}
	// A panel polling gap is unknown time, not a ten-minute recovery interval.
	now = now.Add(time.Hour)
	tick(true)
	if sender.calls.Load() != 1 {
		t.Fatal("missing polling interval caused false recovery")
	}
	for range 10 {
		tick(true)
	}
	if sender.calls.Load() != 2 {
		t.Fatal("fresh stable recovery was not paired")
	}
}

func TestApplyFailureCanNotifyAgainAfterItsCooldown(t *testing.T) {
	sender := &queuedSender{}
	a := NewAlerts(func(context.Context) (bool, string, string, error) { return true, "tok", "42", nil }, sender, slog.New(slog.DiscardHandler))
	n := db.Node{ID: uuid.New(), Name: "apply"}
	a.ApplyFailed(t.Context(), n, "failed")
	a.states[n.ID.String()+"|apply_failed"].LastProblem = time.Now().Add(-2 * time.Hour)
	a.ApplyFailed(t.Context(), n, "failed again")
	if sender.calls.Load() != 2 {
		t.Fatal("successful destination acknowledgement suppressed the next cooldown period")
	}
}

func TestPartialRecoveryReopensOnlyRecoveredChannel(t *testing.T) {
	st := store.OpenTest(t)
	var id uuid.UUID
	if err := st.Pool.QueryRow(t.Context(), `INSERT INTO nodes(name,hostname) VALUES('partial','partial.test') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	sender := &queuedSender{}
	a := NewAlerts(func(context.Context) (bool, string, string, error) { return true, "tok", "42", nil }, sender, slog.New(slog.DiscardHandler))
	var webhookProblems atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Kind string `json:"kind"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if strings.HasSuffix(payload.Kind, "_recovered") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		webhookProblems.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	a.Webhook = &notify.Webhook{URL: srv.URL, Client: srv.Client()}
	s := NewStats(st, nil, time.Hour, slog.New(slog.DiscardHandler))
	s.SetAlerts(a)
	n := db.Node{ID: id, Name: "partial"}
	now := time.Now()
	s.SetNow(func() time.Time { return now })
	tick := func(failed bool) {
		s.recordFindings(t.Context(), n, []reliability.Finding{{Kind: "disk_pressure", Known: true, Failed: failed}})
		if !a.observeReliability(t.Context(), n, true, now) {
			t.Fatal("observation failed")
		}
		a.sendReliability(t.Context(), n)
		now = now.Add(time.Minute)
	}
	for range 6 {
		tick(true)
	}
	for range 11 {
		tick(false)
	}
	if sender.calls.Load() != 2 {
		t.Fatal("Telegram problem/recovery pair was not delivered")
	}
	for range 6 {
		tick(true)
	}
	if sender.calls.Load() != 3 {
		t.Fatalf("failed webhook recovery suppressed Telegram's new problem: %d", sender.calls.Load())
	}
	if webhookProblems.Load() != 1 {
		t.Fatal("unrecovered webhook repeated its active problem")
	}
}

func TestQueuedRecoveryRequiresFreshObservationAtDelivery(t *testing.T) {
	st := store.OpenTest(t)
	var id uuid.UUID
	if err := st.Pool.QueryRow(t.Context(), `INSERT INTO nodes(name,hostname) VALUES('delayed','delayed.test') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	sender := &queuedSender{}
	a := NewAlerts(func(context.Context) (bool, string, string, error) { return true, "tok", "42", nil }, sender, slog.New(slog.DiscardHandler))
	s := NewStats(st, nil, time.Hour, slog.New(slog.DiscardHandler))
	s.SetAlerts(a)
	n := db.Node{ID: id, Name: "delayed"}
	now := time.Now()
	s.SetNow(func() time.Time { return now })
	observe := func(failed bool) {
		s.recordFindings(t.Context(), n, []reliability.Finding{{Kind: "disk_pressure", Known: true, Failed: failed}})
		if !a.observeReliability(t.Context(), n, true, now) {
			t.Fatal("observation failed")
		}
	}
	for range 6 {
		observe(true)
		a.sendReliability(t.Context(), n)
		now = now.Add(time.Minute)
	}
	for range 11 {
		observe(false)
		now = now.Add(time.Minute)
	}
	now = now.Add(3 * time.Minute)
	a.sendReliability(t.Context(), n)
	if sender.calls.Load() != 1 {
		t.Fatalf("queued stale observation sent recovery: %d messages", sender.calls.Load())
	}
}
