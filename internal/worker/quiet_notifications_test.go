package worker_test

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"tgwebproxy/internal/alerttext"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

func TestReliabilityNotificationsAggregateRotatingDCsAndStableRecovery(t *testing.T) {
	f := newFixture(t)
	sender := &fakeSender{}
	driver := nodedriver.NewMock()
	driver.SetOnline(f.node.ID, true)
	now := time.Now().UTC()
	newStats := func() *worker.Stats {
		s := worker.NewStats(f.st, driver, time.Hour, slog.New(slog.DiscardHandler))
		s.SetAlerts(worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler)))
		s.SetNow(func() time.Time { return now })
		return s
	}
	s := newStats()
	sweep := func(bad int, missing bool) {
		r := reliability.Report{Version: 1, At: now, Policy: reliability.DefaultPolicy()}
		if !missing {
			for _, id := range []int{1, 2, 3, 4, 5, 203, -203} {
				alive := 1
				if id == bad || bad == 999 {
					alive = 0
				}
				r.DCs = append(r.DCs, reliability.DC{ID: id, Alive: alive, Required: 1})
			}
		}
		raw, _ := json.Marshal(r)
		health, _ := json.Marshal(nodedriver.HealthReport{Readyz: true, Healthz: true, Reliability: raw})
		if err := f.st.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{ID: f.node.ID, Status: db.NodeStatusOnline, LastHealth: health}); err != nil {
			t.Fatal(err)
		}
		if err := s.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		s.WaitNotifications()
	}
	for i := range 5 {
		bad := 1 + i
		if i == 0 {
			bad = 999
		}
		sweep(bad, false)
		now = now.Add(time.Minute)
	}
	if sender.count() != 0 {
		t.Fatalf("rotating DCs announced before five minutes: %d", sender.count())
	}
	sweep(203, false)
	if sender.count() != 1 {
		t.Fatalf("five minutes of degradation should produce one server message: %d", sender.count())
	}
	if !strings.Contains(sender.last().text, "reliability") {
		t.Fatalf("not a server reliability summary: %s", sender.last().text)
	}
	// Persisted pairing survives a fresh notifier and stats worker.
	s = newStats()
	for range 4 {
		now = now.Add(time.Minute)
		sweep(-203, false)
	}
	if sender.count() != 1 {
		t.Fatalf("new DC or restart repeated the problem: %d", sender.count())
	}
	open, err := f.st.Q.ListOpenAlerts(t.Context())
	if err != nil || len(open) == 0 {
		t.Fatalf("quiet notification erased detailed panel incidents: %v %v", open, err)
	}
	for range 6 {
		now = now.Add(time.Minute)
		sweep(0, false)
	}
	now = now.Add(time.Minute)
	sweep(2, false)
	for range 9 {
		now = now.Add(time.Minute)
		sweep(0, false)
	}
	if sender.count() != 1 {
		t.Fatalf("recovery flap did not reset ten-minute debounce: %d", sender.count())
	}
	// An absent previously failed DC must interrupt recovery continuity.
	now = now.Add(time.Minute)
	sweep(0, true)
	for range 10 {
		now = now.Add(time.Minute)
		sweep(0, false)
	}
	if sender.count() != 1 {
		t.Fatalf("unknown time counted toward recovery: %d", sender.count())
	}
	now = now.Add(time.Minute)
	sweep(0, false)
	if sender.count() != 2 {
		t.Fatalf("stable recovery should pair once after restart: %d", sender.count())
	}
}

func TestPersistedOfflineDeliveryPairsAfterRestartAndRetriesRecovery(t *testing.T) {
	f := newFixture(t)
	sender := &fakeSender{}
	attach := func() *worker.Alerts {
		a := worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler))
		worker.NewStats(f.st, nil, time.Hour, slog.New(slog.DiscardHandler)).SetAlerts(a)
		return a
	}
	a := attach()
	sender.failNextSends(1)
	a.NodeOffline(t.Context(), f.node)
	a = attach()
	a.NodeOffline(t.Context(), f.node)
	a.NodeOffline(t.Context(), f.node)
	if sender.count() != 2 {
		t.Fatalf("problem retry or restart dedup failed: %d", sender.count())
	}
	sender.failNextSends(1)
	a = attach()
	a.NodeOnline(t.Context(), f.node)
	a = attach()
	a.NodeOnline(t.Context(), f.node)
	a.NodeOnline(t.Context(), f.node)
	if sender.count() != 4 {
		t.Fatalf("recovery retry or restart pairing failed: %d", sender.count())
	}
}

func TestConcurrentPersistedAlertsSendOneProblemPerNode(t *testing.T) {
	f := newFixture(t)
	sender := &fakeSender{}
	a := worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler))
	b := worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler))
	worker.NewStats(f.st, nil, time.Hour, slog.New(slog.DiscardHandler)).SetAlerts(a)
	worker.NewStats(f.st, nil, time.Hour, slog.New(slog.DiscardHandler)).SetAlerts(b)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				a.NodeOffline(t.Context(), f.node)
			} else {
				b.NodeOffline(t.Context(), f.node)
			}
		}()
	}
	wg.Wait()
	if sender.count() != 1 {
		t.Fatalf("concurrent sends duplicated the incident: %d", sender.count())
	}
	other, err := f.st.Q.CreateNode(t.Context(), db.CreateNodeParams{Name: "independent", Hostname: "independent.test", AcmeEmail: "a@b.test"})
	if err != nil {
		t.Fatal(err)
	}
	a.NodeOffline(t.Context(), other)
	if sender.count() != 2 {
		t.Fatalf("independent node suppressed: %d", sender.count())
	}
}

func TestStatsOfflineOutageDebounceSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	sender := &fakeSender{}
	driver := nodedriver.NewMock()
	now := time.Now()
	build := func() *worker.Stats {
		s := worker.NewStats(f.st, driver, time.Minute, slog.New(slog.DiscardHandler))
		s.SetNow(func() time.Time { return now })
		s.SetAlerts(worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler)))
		return s
	}
	if _, err := f.st.Pool.Exec(t.Context(), `UPDATE nodes SET status='offline',last_seen_at=now()-interval '10 minutes' WHERE id=$1`, f.node.ID); err != nil {
		t.Fatal(err)
	}
	s := build()
	if err := s.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.WaitNotifications()
	now = now.Add(4 * time.Minute)
	s = build()
	if err := s.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.WaitNotifications()
	if sender.count() != 1 {
		t.Fatalf("restarting panel lost pending offline announcement: %d", sender.count())
	}
	sender.failNextSends(1)
	driver.SetOnline(f.node.ID, true)
	if err := f.st.Q.SetNodeOnline(t.Context(), db.SetNodeOnlineParams{ID: f.node.ID}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		s.WaitNotifications()
	}
	if sender.count() != 3 {
		t.Fatalf("stats lost failed online recovery retry: %d", sender.count())
	}
}

func TestTproxyDiagnosticsAndProbesRetainAggregatedNotifications(t *testing.T) {
	for _, source := range []string{"diagnostics", "probe"} {
		t.Run(source, func(t *testing.T) {
			f := newFixture(t)
			sender := &fakeSender{}
			driver := nodedriver.NewMock()
			driver.SetOnline(f.node.ID, true)
			a := worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler))
			s := worker.NewStats(f.st, driver, time.Hour, slog.New(slog.DiscardHandler))
			s.SetAlerts(a)
			if source == "probe" {
				s.SetProbeLocations([]string{"ams"})
			} else {
				if _, err := f.st.Pool.Exec(t.Context(), `INSERT INTO alerts(node_id,kind,message) VALUES($1,'diagnostic_telemt_agent_link','agent check')`, f.node.ID); err != nil {
					t.Fatal(err)
				}
				a.Incident(t.Context(), f.node, alerttext.Incident{Kind: "diagnostic_telemt_agent_link"})
			}
			now := time.Now()
			s.SetNow(func() time.Time { return now })
			sweep := func(failed bool) {
				health, _ := json.Marshal(nodedriver.HealthReport{Healthz: true, Readyz: true, RelayActive: true})
				if err := f.st.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{ID: f.node.ID, Status: db.NodeStatusOnline, LastHealth: health}); err != nil {
					t.Fatal(err)
				}
				if _, err := f.st.Pool.Exec(t.Context(), `UPDATE nodes SET last_seen_at=$2 WHERE id=$1`, f.node.ID, now); err != nil {
					t.Fatal(err)
				}
				if source == "probe" {
					status := "ok"
					if failed {
						status = "failed"
					}
					raw, _ := json.Marshal(reliability.ProbeReport{Location: "ams", At: now, TLS: reliability.ProbeCheck{Status: status}, HTTP: reliability.ProbeCheck{Status: "not_run"}, FakeTLS: reliability.ProbeCheck{Status: "not_run"}, WEB: reliability.ProbeCheck{Status: "not_run"}})
					if _, err := f.st.Pool.Exec(t.Context(), `INSERT INTO probe_reports(node_id,location,report,measured_at) VALUES($1,'ams',$2,$3) ON CONFLICT(node_id,location) DO UPDATE SET report=excluded.report`, f.node.ID, raw, now); err != nil {
						t.Fatal(err)
					}
				}
				if err := s.RunOnce(t.Context()); err != nil {
					t.Fatal(err)
				}
				s.WaitNotifications()
				now = now.Add(time.Minute)
			}
			for range 6 {
				sweep(true)
			}
			if sender.count() != 1 {
				t.Fatalf("tproxy %s notification suppressed: %d", source, sender.count())
			}
			if source == "diagnostics" {
				if _, err := f.st.Pool.Exec(t.Context(), `UPDATE alerts SET resolved_at=now() WHERE node_id=$1`, f.node.ID); err != nil {
					t.Fatal(err)
				}
			}
			for range 11 {
				sweep(false)
			}
			if sender.count() != 2 {
				t.Fatalf("tproxy %s recovery suppressed: %d", source, sender.count())
			}
		})
	}
}
