package worker_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

func TestParseRelayMetrics(t *testing.T) {
	text := "# HELP tproxy_sessions_live x\n# TYPE tproxy_sessions_live gauge\ntproxy_sessions_live 4\ntproxy_streams_live 12\ntproxy_bytes_up_total 1000\ntproxy_bytes_down_total 2000\ntproxy_sessions_created_total 7\ntproxy_limit_hits_total 1\n"
	m := worker.ParseRelayMetrics(text)
	if m.SessionsLive != 4 || m.StreamsLive != 12 || m.BytesUp != 1000 || m.BytesDown != 2000 || m.SessionsCreated != 7 || m.LimitHits != 1 {
		t.Fatalf("parsed %+v", m)
	}
}

func TestStatsSnapshotAndOffline(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.SetMetrics(f.node.ID, "tproxy_sessions_live 2\ntproxy_streams_live 5\n")
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	snaps, _ := f.st.Q.LatestSnapshots(ctx)
	if len(snaps) != 1 || snaps[0].SessionsLive != 2 || snaps[0].StreamsLive != 5 {
		t.Fatalf("snapshots %+v", snaps)
	}
	// simulate stale heartbeat
	_, _ = f.st.Pool.Exec(ctx, `UPDATE nodes SET last_seen_at = now() - interval '10 minutes'`)
	_ = s.RunOnce(ctx)
	n, _ := f.st.Q.GetNode(ctx, f.node.ID)
	alerts, _ := f.st.Q.ListOpenAlerts(ctx)
	if n.Status != db.NodeStatusOffline || len(alerts) != 1 || alerts[0].Kind != "node_offline" {
		t.Fatalf("offline detection: status=%s alerts=%d", n.Status, len(alerts))
	}
	// back online resolves the alert
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})
	_ = s.RunOnce(ctx)
	alerts, _ = f.st.Q.ListOpenAlerts(ctx)
	if len(alerts) != 0 {
		t.Fatalf("alert not resolved: %+v", alerts)
	}
}

func TestStatsNotifiesOfflineThenOnlineOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})

	sender := &fakeSender{}
	alerts := worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler))
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	s.SetAlerts(alerts)

	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	s.WaitNotifications()
	if sender.count() != 0 {
		t.Fatalf("no notification expected yet, got %d", sender.count())
	}

	// simulate stale heartbeat: offline detection notifies once.
	_, _ = f.st.Pool.Exec(ctx, `UPDATE nodes SET last_seen_at = now() - interval '10 minutes'`)
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	s.WaitNotifications()
	if sender.count() != 1 || !strings.Contains(sender.last().text, "offline") {
		t.Fatalf("expected 1 offline notification, sends=%d last=%q", sender.count(), sender.last().text)
	}

	// back online: resolving the open node_offline alert notifies once.
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	s.WaitNotifications()
	if sender.count() != 2 || !strings.Contains(sender.last().text, "back online") {
		t.Fatalf("expected the 2nd notification to be 'back online', sends=%d last=%q", sender.count(), sender.last().text)
	}

	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	s.WaitNotifications()
	if sender.count() != 2 {
		t.Fatalf("expected no further notification, got %d sends", sender.count())
	}
}

// blockingSender holds every SendWith open until release is closed.
type blockingSender struct {
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (b *blockingSender) SendWith(ctx context.Context, _, _, _ string) error {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return nil
}

func (b *blockingSender) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func TestStatsDoesNotBlockOnSlowNotifications(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})

	sender := &blockingSender{release: make(chan struct{})}
	alerts := worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler))
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	s.SetAlerts(alerts)

	// Stale heartbeat: this tick notifies, and the notifier is wedged.
	_, _ = f.st.Pool.Exec(ctx, `UPDATE nodes SET last_seen_at = now() - interval '10 minutes'`)
	start := time.Now()
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("RunOnce took %v with a wedged notifier; it must not wait on the send", elapsed)
	}
	// The rest of the tick still ran.
	n, _ := f.st.Q.GetNode(ctx, f.node.ID)
	if n.Status != db.NodeStatusOffline {
		t.Fatalf("offline detection did not complete: status=%s", n.Status)
	}

	close(sender.release)
	s.WaitNotifications()
	if sender.count() != 1 {
		t.Fatalf("expected exactly 1 notification attempt, got %d", sender.count())
	}
}

func TestParseTelemtMetrics(t *testing.T) {
	text := strings.Join([]string{
		"# HELP telemt_connections_total Total accepted connections",
		"# TYPE telemt_connections_total counter",
		"telemt_connections_total 41",
		`telemt_user_connections_current{user="k1"} 3`,
		`telemt_user_connections_current{user="k2"} 2`,
		`telemt_user_unique_ips_current{user="k1"} 4`,
		"telemt_connections_bad_total 7",
		"",
	}, "\n")
	m := worker.ParseTelemtMetrics(text)
	if m.ConnectionsTotal != 41 {
		t.Fatalf("connections total: %+v", m)
	}
	if m.ConnectionsLive != 5 {
		t.Fatalf("live connections must be the sum over users: %+v", m)
	}
	if m.Users["k1"].Connections != 3 || m.Users["k1"].UniqueIPs != 4 || m.Users["k2"].Connections != 2 {
		t.Fatalf("per-user: %+v", m.Users)
	}
}

// telemtStats is the stats map an agent on a telemt node returns.
func telemtStats(user string) map[string]string {
	return map[string]string{
		"users": "2", "connections_total": "5", "active_users": "1",
		"user.node.connections": "0", "user.node.octets": "0", "user.node.active_ips": "0",
		"user." + user + ".connections": "3", "user." + user + ".octets": "8192",
		"user." + user + ".active_ips": "2",
	}
}

func TestStatsTelemtNodeWritesNodeAndKeySnapshots(t *testing.T) {
	f := newTelemtFixture(t)
	ctx := context.Background()
	k, err := f.keys.Create(ctx, keys.CreateInput{
		Label: "a", Type: domain.KeyPersonal, CarrierMode: "https",
		TelemtLimits: domain.TelemtLimits{DataQuotaBytes: 10 << 30}, NodeIDs: []uuid.UUID{f.node.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	name := domain.ProfileName(k.ID)
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.SetMetrics(f.node.ID, "telemt_connections_total 41\n"+
		`telemt_user_connections_current{user="`+name+`"} 3`+"\n")
	mock.SetStats(f.node.ID, telemtStats(name))
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	snaps, _ := f.st.Q.LatestSnapshots(ctx)
	if len(snaps) != 1 {
		t.Fatalf("snapshots: %+v", snaps)
	}
	if snaps[0].SessionsLive != 3 || snaps[0].StreamsLive != 3 {
		t.Fatalf("live connections: %+v", snaps[0])
	}
	if snaps[0].BytesDown.Int64 != 8192 || snaps[0].BytesUp.Int64 != 0 || snaps[0].SessionsCreated != 41 {
		t.Fatalf("node snapshot: %+v", snaps[0])
	}

	rows, err := f.st.Q.LatestKeyStatsSnapshots(ctx, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("key snapshots: %+v", rows)
	}
	r := rows[0]
	if r.NodeID != f.node.ID || r.Connections != 3 || r.TotalOctets.Int64 != 8192 || r.ActiveIps != 2 {
		t.Fatalf("key snapshot: %+v", r)
	}
	if r.QuotaUsedBytes.Int64 != 8192 {
		t.Fatalf("a key with a quota must record its usage: %+v", r)
	}
}

func TestStatsTelemtKeySnapshotWithoutQuota(t *testing.T) {
	f := newTelemtFixture(t)
	ctx := context.Background()
	k, _ := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.SetMetrics(f.node.ID, "telemt_connections_total 1\n")
	mock.SetStats(f.node.ID, telemtStats(domain.ProfileName(k.ID)))
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := f.st.Q.LatestKeyStatsSnapshots(ctx, k.ID)
	if len(rows) != 1 || rows[0].QuotaUsedBytes.Int64 != 0 || rows[0].TotalOctets.Int64 != 8192 {
		t.Fatalf("key snapshot: %+v", rows)
	}
	// With no per-user gauge in the metrics the live count falls back to the agent's summary.
	snaps, _ := f.st.Q.LatestSnapshots(ctx)
	if len(snaps) != 1 || snaps[0].SessionsLive != 5 {
		t.Fatalf("fallback live connections: %+v", snaps)
	}
}

// A tproxy node must keep its own parser and must never write per-key rows.
func TestStatsTproxyNodeWritesNoKeyStats(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	k, _ := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.SetMetrics(f.node.ID, "tproxy_sessions_live 2\ntproxy_bytes_up_total 10\ntproxy_bytes_down_total 20\n")
	mock.SetStats(f.node.ID, telemtStats(domain.ProfileName(k.ID)))
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	snaps, _ := f.st.Q.LatestSnapshots(ctx)
	if len(snaps) != 1 || snaps[0].SessionsLive != 2 || snaps[0].BytesUp.Int64 != 10 || snaps[0].BytesDown.Int64 != 20 {
		t.Fatalf("tproxy snapshot: %+v", snaps)
	}
	if rows, _ := f.st.Q.LatestKeyStatsSnapshots(ctx, k.ID); len(rows) != 0 {
		t.Fatalf("tproxy node wrote key stats: %+v", rows)
	}
}

func TestStatsPrunesOldKeyStats(t *testing.T) {
	f := newTelemtFixture(t)
	ctx := context.Background()
	k, _ := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})
	if err := f.st.Q.InsertKeyStatsSnapshot(ctx, db.InsertKeyStatsSnapshotParams{AccessKeyID: k.ID, NodeID: f.node.ID, Connections: 1, TotalOctets: pgtype.Int8{Int64: 1, Valid: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.Pool.Exec(ctx, `UPDATE key_stats_snapshots SET taken_at = now() - interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	mock := nodedriver.NewMock()
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if rows, _ := f.st.Q.LatestKeyStatsSnapshots(ctx, k.ID); len(rows) != 0 {
		t.Fatalf("retention did not prune: %+v", rows)
	}
}

func TestStatsSnapshotCarriesNodeLoad(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.SetMetrics(f.node.ID, "tproxy_sessions_live 1\n")
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})
	raw, _ := json.Marshal(nodedriver.HealthReport{RelayActive: true, CPUPercent: 42.5, MemUsedPercent: 61, DiskUsedPercent: 12.25})
	if err := f.st.Q.SetNodeHeartbeat(ctx, db.SetNodeHeartbeatParams{ID: f.node.ID, Status: db.NodeStatusOnline, LastHealth: raw}); err != nil {
		t.Fatal(err)
	}
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	snaps, _ := f.st.Q.LatestSnapshots(ctx)
	if len(snaps) != 1 {
		t.Fatalf("snapshots %+v", snaps)
	}
	if got := snaps[0]; got.CpuPercent.Float32 != 42.5 || got.MemUsedPercent != 61 || got.DiskUsedPercent != 12.25 {
		t.Fatalf("load not carried into the snapshot: cpu=%v mem=%v disk=%v", got.CpuPercent, got.MemUsedPercent, got.DiskUsedPercent)
	}

	// A node that has never reported health (empty last_health) still snapshots, with zero load.
	_, _ = f.st.Pool.Exec(ctx, `UPDATE nodes SET last_health = '{}'::jsonb WHERE id = $1`, f.node.ID)
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	snaps, _ = f.st.Q.LatestSnapshots(ctx)
	if len(snaps) != 1 || snaps[0].CpuPercent.Float32 != 0 {
		t.Fatalf("snapshot without health: %+v", snaps)
	}
}

func TestStatsSnapshotCarriesDcLatency(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.SetMetrics(f.node.ID, "tproxy_sessions_live 1\n")
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})
	raw, _ := json.Marshal(nodedriver.HealthReport{
		RelayActive: true, DcDataAvailable: true, UpstreamHealthy: true, EffectiveLatencyMs: 41.25,
		DCs: []nodedriver.DcLatency{
			{DC: 1, LatencyMs: 197.9, Known: true, IPPreference: "prefer_v4"},
			{DC: 2, LatencyMs: 36.5, Known: true, IPPreference: "prefer_v4"},
			{DC: 4, Known: false, IPPreference: "prefer_v6"},
		},
	})
	if err := f.st.Q.SetNodeHeartbeat(ctx, db.SetNodeHeartbeatParams{ID: f.node.ID, Status: db.NodeStatusOnline, LastHealth: raw}); err != nil {
		t.Fatal(err)
	}
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	snaps, _ := f.st.Q.LatestSnapshots(ctx)
	if len(snaps) != 1 {
		t.Fatalf("snapshots %+v", snaps)
	}
	var got map[string]float64
	if err := json.Unmarshal(snaps[0].DcLatency, &got); err != nil {
		t.Fatalf("dc_latency %s: %v", snaps[0].DcLatency, err)
	}
	if len(got) != 2 || got["1"] != 197.9 || got["2"] != 36.5 {
		t.Fatalf("dc_latency = %s, want DC 1 and 2 only (DC 4 is unknown)", snaps[0].DcLatency)
	}
	if _, present := got["4"]; present {
		t.Fatalf("an unknown DC must be omitted, not written as 0: %s", snaps[0].DcLatency)
	}

	raw, _ = json.Marshal(nodedriver.HealthReport{RelayActive: true, DCs: []nodedriver.DcLatency{{DC: 1, LatencyMs: 5, Known: true}}})
	if err := f.st.Q.SetNodeHeartbeat(ctx, db.SetNodeHeartbeatParams{ID: f.node.ID, Status: db.NodeStatusOnline, LastHealth: raw}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	snaps, _ = f.st.Q.LatestSnapshots(ctx)
	if len(snaps) != 1 || string(snaps[0].DcLatency) != "{}" {
		t.Fatalf("snapshot without DC data: %+v", snaps)
	}
}

// Consumption is the difference between two snapshots. A reading that failed used to be stored as
// zero, so 100 -> failed -> 110 was charged as 110 bytes instead of 10, on both the node's chart
// and the key's quota. The row is still written - the gauges beside the counters were read fine -
// but the counters say they were not measured.
func TestStatsRecordsTrafficThatCouldNotBeReadAsNotMeasured(t *testing.T) {
	f := newTelemtFixture(t)
	ctx := context.Background()
	k, err := f.keys.Create(ctx, keys.CreateInput{
		Label: "a", Type: domain.KeyPersonal, CarrierMode: "https",
		TelemtLimits: domain.TelemtLimits{DataQuotaBytes: 10 << 30}, NodeIDs: []uuid.UUID{f.node.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	name := domain.ProfileName(k.ID)
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.SetMetrics(f.node.ID, "telemt_connections_total 41\n"+
		`telemt_user_connections_current{user="`+name+`"} 3`+"\n")
	mock.SetStats(f.node.ID, telemtStats(name))
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))

	if err := s.RunOnce(ctx); err != nil { // one good reading to have something to compare against
		t.Fatal(err)
	}

	mock.SetStatsErr(f.node.ID, "connection refused")
	if err := s.RunOnce(ctx); err != nil {
		t.Fatalf("a node that cannot report traffic must not fail the sweep: %v", err)
	}

	snaps, _ := f.st.Q.LatestSnapshots(ctx)
	if len(snaps) != 1 {
		t.Fatalf("snapshots: %+v", snaps)
	}
	if snaps[0].BytesDown.Valid || snaps[0].BytesUp.Valid {
		t.Fatalf("bytes = %+v/%+v, want them marked as not measured rather than zero",
			snaps[0].BytesUp, snaps[0].BytesDown)
	}
	// The gauges came from the metrics text, which was read: only the counters are missing.
	if snaps[0].SessionsLive == 0 {
		t.Fatalf("the readings that did succeed were thrown away with the ones that did not: %+v", snaps[0])
	}

	rows, err := f.st.Q.LatestKeyStatsSnapshots(ctx, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("key rows = %+v", rows)
	}
	if rows[0].TotalOctets.Valid {
		t.Fatalf("key octets = %+v, want them marked as not measured", rows[0].TotalOctets)
	}
	if rows[0].Connections == 0 {
		t.Fatal("the connection count was known and should have been kept")
	}
}

// slowMetricsDriver makes the named nodes hang in Metrics until released, the way an agent that
// has stopped answering holds a request open for the driver's whole timeout.
type slowMetricsDriver struct {
	*nodedriver.Mock
	slow    map[uuid.UUID]bool
	release chan struct{}
}

func (d *slowMetricsDriver) Metrics(ctx context.Context, id uuid.UUID) (string, error) {
	if d.slow[id] {
		select {
		case <-d.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return d.Mock.Metrics(ctx, id)
}

// A node that has stopped answering must not hold up everybody else's snapshot.
func TestStatsPollsNodesTogether(t *testing.T) {
	f := newTelemtFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	slow := map[uuid.UUID]bool{}

	ids := []uuid.UUID{f.node.ID}
	for i := range 2 {
		n, err := f.st.Q.CreateNode(ctx, db.CreateNodeParams{
			Name: "extra" + strconv.Itoa(i), Hostname: "extra" + strconv.Itoa(i) + ".test",
			AcmeEmail: "a@b.co", Engine: db.NullNodeEngine{NodeEngine: db.NodeEngineTelemt, Valid: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	for i, id := range ids {
		mock.SetOnline(id, true)
		mock.SetMetrics(id, "telemt_connections_total 1\n")
		mock.SetStats(id, map[string]string{"connections_total": "1"})
		_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: id})
		if i < 2 {
			// The stuck ones come first, so a sequential sweep would make the healthy node
			// wait behind them and this test would have nothing to say.
			slow[id] = true
		}
	}

	drv := &slowMetricsDriver{Mock: mock, slow: slow, release: make(chan struct{})}
	s := worker.NewStats(f.st, drv, 90*time.Second, slog.New(slog.DiscardHandler))

	done := make(chan error, 1)
	go func() { done <- s.RunOnce(ctx) }()

	// The healthy node's snapshot must land while the other two are still hanging.
	deadline := time.After(5 * time.Second)
	for {
		snaps, _ := f.st.Q.LatestSnapshots(ctx)
		if len(snaps) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the healthy node waited behind the stuck ones")
		case <-time.After(20 * time.Millisecond):
		}
	}

	close(drv.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the sweep never finished")
	}
}
