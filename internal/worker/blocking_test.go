package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

type blockingSender struct {
	queuedSender
	last string
}

func (s *blockingSender) SendWith(_ context.Context, _, _, msg string) error {
	s.calls.Add(1)
	s.last = msg
	return nil
}

type blockingFixture struct {
	s              *Stats
	a              *Alerts
	n              db.Node
	now            time.Time
	sender         *blockingSender
	cuts, accepted int
	web            bool
}

func newBlockingFixture(t *testing.T) *blockingFixture {
	t.Helper()
	st := store.OpenTest(t)
	id := uuid.New()
	if _, e := st.Pool.Exec(t.Context(), `INSERT INTO nodes(id,name,hostname,engine) VALUES($1,'blocking','blocking.test','telemt')`, id); e != nil {
		t.Fatal(e)
	}
	f := &blockingFixture{n: db.Node{ID: id, Name: "blocking", Engine: db.NodeEngineTelemt}, now: time.Now(), sender: &blockingSender{}}
	f.s = NewStats(st, nil, time.Hour, slog.New(slog.DiscardHandler))
	f.s.SetNow(func() time.Time { return f.now })
	f.a = NewAlerts(func(context.Context) (bool, string, string, error) { return true, "tok", "42", nil }, f.sender, slog.New(slog.DiscardHandler))
	f.s.SetAlerts(f.a)
	f.s.SetProbeLocations([]string{"isp"})
	return f
}

func (f *blockingFixture) report(t *testing.T, status, method string, web bool) {
	t.Helper()
	check := map[string]any{"status": status, "latency_ms": 1}
	if method != "" {
		check["method"] = method
	}
	unused := map[string]any{"status": "not_run", "latency_ms": 0}
	p := map[string]any{"node_id": f.n.ID.String(), "location": "isp", "at": f.now, "tls": unused, "http": unused, "faketls": check, "web": unused}
	if web {
		p["web"] = map[string]any{"status": "ok", "latency_ms": 1, "method": "authenticated_mtproto"}
	}
	raw, _ := json.Marshal(p)
	if _, e := f.s.st.Pool.Exec(t.Context(), `INSERT INTO probe_reports(node_id,location,measured_at,report) VALUES($1,'isp',$2,$3) ON CONFLICT(node_id,location) DO UPDATE SET measured_at=excluded.measured_at,report=excluded.report`, f.n.ID, f.now, raw); e != nil {
		t.Fatal(e)
	}
}

func (f *blockingFixture) tick(t *testing.T, method, status string) {
	t.Helper()
	f.report(t, status, method, f.web)
	f.s.recordFindings(t.Context(), f.n, []reliability.Finding{{Kind: "disk_pressure", Known: true}})
	f.s.observeBlocking(t.Context(), f.n, fmt.Sprintf("telemt_connections_total %d\ntelemt_handshake_failures_by_class_total{class=\"timeout\"} %d\n", f.accepted, f.cuts))
	if !f.a.observeReliability(t.Context(), f.n, true, f.now) {
		t.Fatal("observe failed")
	}
	f.a.sendReliability(t.Context(), f.n)
	f.now = f.now.Add(time.Minute)
	f.cuts += 40
	f.accepted += 60
}

func (f *blockingFixture) open(t *testing.T) int {
	t.Helper()
	var n int
	if e := f.s.st.Pool.QueryRow(t.Context(), `SELECT count(*) FROM alerts WHERE node_id=$1 AND kind='reliability_looks_like_blocking' AND resolved_at IS NULL`, f.n.ID).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}

func TestBlockingRequiresAuthenticatedCorroboration(t *testing.T) {
	for _, tc := range []struct{ method, status string }{{"", "failed"}, {"authenticated_mtproto", "ok"}, {"authenticated_mtproto", "not_run"}} {
		t.Run(tc.method+tc.status, func(t *testing.T) {
			f := newBlockingFixture(t)
			for range 10 {
				f.tick(t, tc.method, tc.status)
			}
			if f.open(t) != 0 || f.sender.calls.Load() != 0 {
				t.Fatal("scanner traffic became a blocking incident")
			}
		})
	}
	f := newBlockingFixture(t)
	for range 9 {
		f.tick(t, "authenticated_mtproto", "failed")
	}
	if f.open(t) != 1 || f.sender.calls.Load() != 1 {
		t.Fatalf("corroborated sustained degradation: open=%d sends=%d", f.open(t), f.sender.calls.Load())
	}
	var known bool
	if e := f.s.st.Pool.QueryRow(t.Context(), `SELECT known FROM notification_findings WHERE node_id=$1 AND kind='disk_pressure'`, f.n.ID).Scan(&known); e != nil || !known {
		t.Fatalf("blocking replaced health: known=%v err=%v", known, e)
	}
}

func TestBlockingUnknownDoesNotRecoverAcrossRestart(t *testing.T) {
	f := newBlockingFixture(t)
	for range 9 {
		f.tick(t, "authenticated_mtproto", "failed")
	}
	f.s = NewStats(f.s.st, nil, time.Hour, slog.New(slog.DiscardHandler))
	f.s.SetNow(func() time.Time { return f.now })
	f.s.SetAlerts(f.a)
	f.s.SetProbeLocations([]string{"isp"})
	// WEB still works but the previously executed FakeTLS check disappeared.
	for range 12 {
		f.report(t, "not_run", "", true)
		f.s.observeBlocking(t.Context(), f.n, fmt.Sprintf("telemt_connections_total %d\ntelemt_handshake_failures_by_class_total{class=\"timeout\"} %d\n", f.accepted, f.cuts))
		f.a.observeReliability(t.Context(), f.n, true, f.now)
		f.a.sendReliability(t.Context(), f.n)
		f.now = f.now.Add(time.Minute)
		f.accepted += 60
		f.cuts += 40
	}
	if f.open(t) != 1 || f.sender.calls.Load() != 1 {
		t.Fatal("disappeared transport recovered the incident")
	}
	f.web = true
	for range 11 {
		f.tick(t, "authenticated_mtproto", "ok")
	}
	if f.open(t) != 0 || f.sender.calls.Load() != 2 {
		t.Fatalf("verified stable recovery: open=%d sends=%d", f.open(t), f.sender.calls.Load())
	}
}

func TestBlockingNeverEnabledDoesNotPreventOtherRecovery(t *testing.T) {
	f := newBlockingFixture(t)
	f.s.SetProbeLocations(nil)
	for i := 0; i < 18; i++ {
		f.s.recordFindings(t.Context(), f.n, []reliability.Finding{{Kind: "disk_pressure", Known: true, Failed: i < 6}})
		f.s.observeBlocking(t.Context(), f.n, "")
		f.a.observeReliability(t.Context(), f.n, true, f.now)
		f.a.sendReliability(t.Context(), f.n)
		f.now = f.now.Add(time.Minute)
	}
	if f.sender.calls.Load() != 2 {
		t.Fatalf("unconfigured blocking prevented recovery: %d", f.sender.calls.Load())
	}
}

func TestBlockingSummaryIncludesConditionalReason(t *testing.T) {
	f := newBlockingFixture(t)
	for range 9 {
		f.tick(t, "authenticated_mtproto", "failed")
	}
	if !strings.Contains(f.sender.last, "filtering") || !strings.Contains(f.sender.last, "configuration") {
		t.Fatalf("summary lacks possible filtering/server/configuration explanation: %s", f.sender.last)
	}
}

func TestBlockingExplicitRemovalRetiresAggregateObservation(t *testing.T) {
	f := newBlockingFixture(t)
	for range 9 {
		f.tick(t, "authenticated_mtproto", "failed")
	}
	f.s.SetProbeLocations(nil)
	f.s.retireBlockingEvidence(t.Context(), f.n)
	var n int
	if e := f.s.st.Pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_findings WHERE node_id=$1 AND source IN ('blocking','blocking_evidence')`, f.n.ID).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 0 || f.open(t) != 1 {
		t.Fatalf("retirement should remove aggregate eligibility without claiming incident recovery: rows=%d open=%d", n, f.open(t))
	}
}

func TestBlockingSweepUsesCurrentEvidenceAndQuietDeliveryAcrossRestart(t *testing.T) {
	f := newBlockingFixture(t)
	driver := nodedriver.NewMock()
	driver.SetOnline(f.n.ID, true)
	f.s.driver = driver
	sweep := func(status string) {
		t.Helper()
		f.report(t, status, "authenticated_mtproto", false)
		r, _ := json.Marshal(reliability.Report{Version: 1, At: f.now})
		health, _ := json.Marshal(nodedriver.HealthReport{Readyz: true, Healthz: true, Reliability: r})
		if e := f.s.st.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{ID: f.n.ID, Status: db.NodeStatusOnline, LastHealth: health}); e != nil {
			t.Fatal(e)
		}
		driver.SetMetrics(f.n.ID, fmt.Sprintf("telemt_connections_total %d\ntelemt_handshake_failures_by_class_total{class=\"timeout\"} %d\n", f.accepted, f.cuts))
		if e := f.s.RunOnce(t.Context()); e != nil {
			t.Fatal(e)
		}
		f.s.WaitNotifications()
		f.now = f.now.Add(time.Minute)
		f.cuts += 40
		f.accepted += 60
	}
	for range 6 {
		sweep("failed")
	}
	if f.open(t) != 1 || f.sender.calls.Load() != 1 || !strings.Contains(f.sender.last, "filtering") {
		t.Fatalf("current sweep evidence missing: open=%d sends=%d message=%s", f.open(t), f.sender.calls.Load(), f.sender.last)
	}
	f.s = NewStats(f.s.st, driver, time.Hour, slog.New(slog.DiscardHandler))
	f.s.SetNow(func() time.Time { return f.now })
	f.s.SetAlerts(f.a)
	f.s.SetProbeLocations([]string{"isp"})
	for range 4 {
		sweep("failed")
	}
	if f.sender.calls.Load() != 1 {
		t.Fatal("restart repeated delivered episode")
	}
	for range 11 {
		sweep("ok")
	}
	if f.open(t) != 0 || f.sender.calls.Load() != 2 {
		t.Fatalf("stable recovery not paired once: open=%d sends=%d", f.open(t), f.sender.calls.Load())
	}
}

func TestBlockingStaleFutureAndPublicChecksCannotCorroborate(t *testing.T) {
	for _, tc := range []string{"stale", "future", "missing", "public"} {
		t.Run(tc, func(t *testing.T) {
			f := newBlockingFixture(t)
			for range 9 {
				f.report(t, "failed", "authenticated_mtproto", false)
				switch tc {
				case "missing":
					if _, e := f.s.st.Pool.Exec(t.Context(), `DELETE FROM probe_reports WHERE node_id=$1`, f.n.ID); e != nil {
						t.Fatal(e)
					}
				case "public":
					if _, e := f.s.st.Pool.Exec(t.Context(), `UPDATE probe_reports SET report=jsonb_set(jsonb_set(report,'{tls}',report->'faketls'),'{faketls}','{"status":"not_run","latency_ms":0}'::jsonb) WHERE node_id=$1`, f.n.ID); e != nil {
						t.Fatal(e)
					}
				default:
					age := -4 * time.Minute
					if tc == "future" {
						age = time.Minute
					}
					raw, _ := json.Marshal(f.now.Add(age))
					if _, e := f.s.st.Pool.Exec(t.Context(), `UPDATE probe_reports SET report=jsonb_set(report,'{at}',$2) WHERE node_id=$1`, f.n.ID, raw); e != nil {
						t.Fatal(e)
					}
				}
				f.s.observeBlocking(t.Context(), f.n, fmt.Sprintf("telemt_connections_total %d\ntelemt_handshake_failures_by_class_total{class=\"timeout\"} %d\n", f.accepted, f.cuts))
				f.now = f.now.Add(time.Minute)
				f.cuts += 40
				f.accepted += 60
			}
			if f.open(t) != 0 {
				t.Fatal("invalid/public observation corroborated filtering")
			}
		})
	}
}

func TestBlockingWarmupAndMissingMetricsPreserveExecutedTransportHistory(t *testing.T) {
	for _, missingMetrics := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing-metrics-%v", missingMetrics), func(t *testing.T) {
			f := newBlockingFixture(t)
			f.report(t, "ok", "authenticated_mtproto", true)
			text := "telemt_connections_total 0\ntelemt_handshake_failures_by_class_total{class=\"timeout\"} 0\n"
			if missingMetrics {
				text = ""
			}
			f.s.observeBlocking(t.Context(), f.n, text)
			step := func(webStatus string, both bool) {
				t.Helper()
				status, method := "not_run", ""
				if both {
					status, method = "ok", "authenticated_mtproto"
				}
				f.report(t, status, method, true)
				if _, e := f.s.st.Pool.Exec(t.Context(), `UPDATE probe_reports SET report=jsonb_set(report,'{web,status}',to_jsonb($2::text)) WHERE node_id=$1`, f.n.ID, webStatus); e != nil {
					t.Fatal(e)
				}
				f.s.recordFindings(t.Context(), f.n, []reliability.Finding{{Kind: "disk_pressure", Known: true}})
				f.s.observeBlocking(t.Context(), f.n, fmt.Sprintf("telemt_connections_total %d\ntelemt_handshake_failures_by_class_total{class=\"timeout\"} %d\n", f.accepted, f.cuts))
				f.a.observeReliability(t.Context(), f.n, true, f.now)
				f.a.sendReliability(t.Context(), f.n)
				f.now = f.now.Add(time.Minute)
				f.accepted += 60
				f.cuts += 40
			}
			f.now = f.now.Add(time.Minute)
			f.accepted = 60
			f.cuts = 40
			for range 9 {
				step("failed", false)
			}
			if f.open(t) != 1 || f.sender.calls.Load() != 1 {
				t.Fatal("corroborated degradation did not open")
			}
			f.s = NewStats(f.s.st, nil, time.Hour, slog.New(slog.DiscardHandler))
			f.s.SetNow(func() time.Time { return f.now })
			f.s.SetAlerts(f.a)
			f.s.SetProbeLocations([]string{"isp"})
			for range 18 {
				step("ok", false)
			}
			if f.open(t) != 1 || f.sender.calls.Load() != 1 {
				t.Fatal("FakeTLS seen before usable metrics disappeared and falsely recovered after restart")
			}
			for range 11 {
				step("ok", true)
			}
			if f.open(t) != 0 || f.sender.calls.Load() != 2 {
				t.Fatal("both authenticated transports healthy did not recover")
			}
		})
	}
}
