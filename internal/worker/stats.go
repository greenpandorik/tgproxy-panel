package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

type RelayMetrics struct {
	SessionsLive, StreamsLive                      int
	BytesUp, BytesDown, SessionsCreated, LimitHits int64
}

// ParseRelayMetrics reads the handful of relay metrics we chart. Prometheus text format, no labels.
func ParseRelayMetrics(text string) RelayMetrics {
	var m RelayMetrics
	for _, line := range strings.Split(text, "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		name, val, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			continue
		}
		switch name {
		case "tproxy_sessions_live":
			m.SessionsLive = int(f)
		case "tproxy_streams_live":
			m.StreamsLive = int(f)
		case "tproxy_bytes_up_total":
			m.BytesUp = int64(f)
		case "tproxy_bytes_down_total":
			m.BytesDown = int64(f)
		case "tproxy_sessions_created_total":
			m.SessionsCreated = int64(f)
		case "tproxy_limit_hits_total":
			m.LimitHits = int64(f)
		}
	}
	return m
}

// TelemtUserMetrics is one user's slice of the telemt Prometheus endpoint.
type TelemtUserMetrics struct {
	Connections int
	UniqueIPs   int
}

// TelemtMetrics is the telemt equivalent of RelayMetrics. telemt has no single "live
// connections" gauge, so the live figure is the sum of the per-user gauges; that series is
// capped at a fixed number of users on telemt's side, which is why the per-user rows the
// panel stores come from the control API (the agent's stats map) and only fall back to these.
type TelemtMetrics struct {
	ConnectionsTotal int64
	ConnectionsLive  int
	Users            map[string]TelemtUserMetrics
}

// ParseTelemtMetrics reads the telemt_* metrics the panel charts. The per-user series carry a
// single `user` label; every other labelled series is ignored.
func ParseTelemtMetrics(text string) TelemtMetrics {
	m := TelemtMetrics{Users: map[string]TelemtUserMetrics{}}
	for _, line := range strings.Split(text, "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		name, val, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			continue
		}
		metric, user := splitUserLabel(name)
		switch metric {
		case "telemt_connections_total":
			m.ConnectionsTotal = int64(f)
		case "telemt_user_connections_current":
			if user == "" {
				continue
			}
			u := m.Users[user]
			u.Connections = int(f)
			m.Users[user] = u
			m.ConnectionsLive += int(f)
		case "telemt_user_unique_ips_current":
			if user == "" {
				continue
			}
			u := m.Users[user]
			u.UniqueIPs = int(f)
			m.Users[user] = u
		}
	}
	return m
}

// splitUserLabel splits `telemt_user_x{user="k1"}` into the metric name and the user. A series
// with any other label set is returned with an empty user, so it is skipped by the caller.
func splitUserLabel(name string) (metric, user string) {
	metric, labels, ok := strings.Cut(name, "{")
	if !ok {
		return name, ""
	}
	labels = strings.TrimSuffix(labels, "}")
	rest, ok := strings.CutPrefix(labels, `user="`)
	if !ok {
		return metric, ""
	}
	user, _, ok = strings.Cut(rest, `"`)
	if !ok {
		return metric, ""
	}
	return metric, user
}

// telemtRelayMetrics maps a telemt node onto the engine-agnostic snapshot columns.
//
// sessions_live and streams_live both carry the live connection count: telemt has no notion of
// streams inside a session, and leaving streams_live at zero would break the existing charts.
// telemt reports exactly one cumulative octet counter per user (traffic in both directions
// together), so the total lands in bytes_down and bytes_up stays 0 rather than inventing a
// split - the node stats tab labels it accordingly.
func telemtRelayMetrics(m TelemtMetrics, stats map[string]string) RelayMetrics {
	live := m.ConnectionsLive
	if len(m.Users) == 0 {
		// Per-user telemetry is off (or the series was capped away): the agent's summary of
		// the control API is then the only live figure available.
		live = int(statsInt(stats, "connections_total"))
	}
	out := RelayMetrics{SessionsLive: live, StreamsLive: live, SessionsCreated: m.ConnectionsTotal}
	for k, v := range stats {
		name, ok := strings.CutPrefix(k, "user.")
		if !ok {
			continue
		}
		if !strings.HasSuffix(name, ".octets") {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		out.BytesDown += n
	}
	return out
}

func statsInt(stats map[string]string, key string) int64 {
	n, err := strconv.ParseInt(stats[key], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

type Stats struct {
	st               *store.Store
	driver           nodedriver.Driver
	offlineAfter     time.Duration
	offlineAfterFunc func(context.Context) time.Duration
	alerts           *Alerts
	log              *slog.Logger

	// notifySlots bounds how many alert notifications may be in flight at once;
	// notifyWG lets a test wait for them. See notify.
	notifySlots chan struct{}
	notifyWG    sync.WaitGroup

	// lastKeyStatsSweep is when the key_stats_snapshots retention sweep last ran. The sweep
	// is the only statement that touches the whole table, and nothing depends on it being
	// prompt, so it runs on keyStatsSweepEvery rather than on every tick. now() is a hook so
	// a test does not have to wait ten minutes.
	lastKeyStatsSweep time.Time
	now               func() time.Time
}

// maxInFlightNotifications bounds the goroutines RunOnce may have out sending Telegram
// messages. A fleet cannot produce more simultaneous offline/online transitions than this
// without the rate limiter kicking in, and anything beyond it is dropped rather than queued -
// the (node, kind) slot is only consumed by a *successful* send, so a dropped notification is
// simply retried on the next tick.
const maxInFlightNotifications = 32

func NewStats(st *store.Store, driver nodedriver.Driver, offlineAfter time.Duration, log *slog.Logger) *Stats {
	return &Stats{
		st: st, driver: driver, offlineAfter: offlineAfter, log: log,
		notifySlots: make(chan struct{}, maxInFlightNotifications),
	}
}

// notify runs one alert notification off RunOnce's goroutine.
//
// The shared Telegram client has a 10s timeout, and that bound is per call, not per tick:
// when the panel's own uplink hiccups every node goes stale at once and inline sends stall
// RunOnce for N x 10s - delaying the snapshots and the recovery detection that would clear
// the very alerts being sent. RunOnce must therefore never wait on a network round trip.
func (s *Stats) notify(ctx context.Context, fn func(context.Context)) {
	select {
	case s.notifySlots <- struct{}{}:
	default:
		s.log.Warn("alert notification dropped: too many already in flight", "limit", maxInFlightNotifications)
		return
	}
	s.notifyWG.Add(1)
	go func() {
		defer s.notifyWG.Done()
		defer func() { <-s.notifySlots }()
		fn(ctx)
	}()
}

// WaitNotifications blocks until every notification spawned so far has finished. Production
// code never needs it - the notifier is deliberately fire-and-forget - but a test asserting
// on what was sent must not race the send.
func (s *Stats) WaitNotifications() { s.notifyWG.Wait() }

// SetOfflineAfterFunc installs an override consulted on every RunOnce to
// decide the stale-heartbeat threshold; a nil or non-positive result falls
// back to the constructor's value. Nil (the default) keeps it fixed.
func (s *Stats) SetOfflineAfterFunc(f func(context.Context) time.Duration) {
	s.offlineAfterFunc = f
}

// SetAlerts installs the Telegram alert notifier; nil (the default) means no
// notifications are sent.
func (s *Stats) SetAlerts(a *Alerts) { s.alerts = a }

// RunOnce marks nodes with a stale heartbeat offline (raising a node_offline
// alert), takes a stats snapshot for every online node, resolves offline
// alerts for nodes that are back, and prunes snapshots older than 30 days.
func (s *Stats) RunOnce(ctx context.Context) error {
	offlineAfter := s.offlineAfter
	if s.offlineAfterFunc != nil {
		if v := s.offlineAfterFunc(ctx); v > 0 {
			offlineAfter = v
		}
	}
	// 1. offline detection
	stale, err := s.st.Q.MarkStaleNodesOffline(ctx, ptrTime(time.Now().Add(-offlineAfter)))
	if err != nil {
		return err
	}
	for _, id := range stale {
		if _, err := s.st.Q.InsertAlert(ctx, db.InsertAlertParams{NodeID: nullUUID(id), Kind: "node_offline", Message: "node stopped sending heartbeats"}); err != nil {
			s.log.Error("insert alert", "err", err)
		}
		if node, err := s.st.Q.GetNode(ctx, id); err == nil {
			s.notify(ctx, func(ctx context.Context) { s.alerts.NodeOffline(ctx, node) })
		}
	}
	// 2. snapshots for online nodes; resolve offline alerts
	nodes, err := s.st.Q.ListNodes(ctx)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.Status == db.NodeStatusOffline || n.Status == db.NodeStatusPending {
			continue
		}
		if rows, err := s.st.Q.ResolveNodeAlerts(ctx, db.ResolveNodeAlertsParams{NodeID: nullUUID(n.ID), Kind: "node_offline"}); err == nil && rows > 0 {
			s.notify(ctx, func(ctx context.Context) { s.alerts.NodeOnline(ctx, n) })
		}
		if !s.driver.Online(n.ID) {
			continue
		}
		text, err := s.driver.Metrics(ctx, n.ID)
		if err != nil {
			s.log.Warn("metrics", "node", n.ID, "err", err)
			continue
		}
		stats, _ := s.driver.Stats(ctx, n.ID)
		m := ParseRelayMetrics(text)
		var telemtM TelemtMetrics
		if n.Engine == db.NodeEngineTelemt {
			telemtM = ParseTelemtMetrics(text)
			m = telemtRelayMetrics(telemtM, stats)
		}
		raw, _ := json.Marshal(stats)
		if raw == nil {
			raw = []byte("{}")
		}
		if err := s.st.Q.InsertSnapshot(ctx, db.InsertSnapshotParams{
			NodeID: n.ID, SessionsLive: int32(m.SessionsLive), StreamsLive: int32(m.StreamsLive),
			BytesUp: m.BytesUp, BytesDown: m.BytesDown, SessionsCreated: m.SessionsCreated, LimitHits: m.LimitHits,
			// relay_raw is deliberately left empty: nothing reads it, and storing the full
			// Prometheus text every 60s for 30 days is ~200 MB per node of write-only data.
			// The column is kept (reserved) — the parsed columns above carry everything the
			// UI uses, and mtproxy_raw is read by the node stats tab.
			MtproxyRaw: raw, RelayRaw: "",
		}); err != nil {
			s.log.Error("snapshot", "err", err)
		}
		if n.Engine == db.NodeEngineTelemt {
			if err := s.keySnapshots(ctx, n.ID, telemtM, stats); err != nil {
				s.log.Error("key snapshot", "node", n.ID, "err", err)
			}
		}
	}
	// Expired sessions are filtered out by GetSession but were never deleted, so the table
	// grew forever. This is the only per-minute sweep the panel runs, so it lives here.
	if err := s.st.Q.DeleteExpiredSessions(ctx); err != nil {
		s.log.Error("delete expired sessions", "err", err)
	}
	s.sweepKeyStats(ctx)
	return s.st.Q.DeleteOldSnapshots(ctx, time.Now().Add(-retention))
}

// keyStatsSweepEvery is how often the key_stats_snapshots retention sweep runs, and
// keyStatsSweepBatch how many rows one DELETE statement may take.
//
// The table gets one row per (key, node) per minute, so it is by far the largest in the
// database and its expired tail is the only thing a sweep has to remove. Running it on every
// 60s tick bought nothing - a minute's worth of rows falls out of the window each time - while
// paying for a full pass over the table's dead tail every minute. Ten minutes keeps the table
// just as bounded, and the batches keep each statement short enough that its locks and WAL
// burst are not felt by the rest of the panel.
const (
	keyStatsSweepEvery = 10 * time.Minute
	keyStatsSweepBatch = 5000
	// keyStatsSweepMaxBatches bounds one sweep so a table that is far behind (a long panel
	// outage, a retention change) cannot hold the worker for an unbounded time; whatever is
	// left is taken by the next sweep.
	keyStatsSweepMaxBatches = 40
)

func (s *Stats) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// sweepKeyStats deletes expired key_stats_snapshots rows in bounded batches, at most once per
// keyStatsSweepEvery.
func (s *Stats) sweepKeyStats(ctx context.Context) {
	now := s.clock()
	if !s.lastKeyStatsSweep.IsZero() && now.Sub(s.lastKeyStatsSweep) < keyStatsSweepEvery {
		return
	}
	s.lastKeyStatsSweep = now
	before := now.Add(-retention)
	for i := 0; i < keyStatsSweepMaxBatches; i++ {
		n, err := s.st.Q.DeleteOldKeyStatsSnapshots(ctx, db.DeleteOldKeyStatsSnapshotsParams{
			Before: before, Batch: keyStatsSweepBatch,
		})
		if err != nil {
			s.log.Error("delete old key stats", "err", err)
			return
		}
		if n < keyStatsSweepBatch {
			return
		}
	}
	s.log.Warn("key stats retention sweep hit its batch limit; the rest is taken by the next sweep")
}

// retention is how long both snapshot tables are kept. The key stats endpoint caps the range
// it will serve just above it, so nothing the UI can ask for has been pruned away.
const retention = 30 * 24 * time.Hour

// keySnapshots writes one key_stats_snapshots row per key that telemt reported on this node.
//
// The per-user figures come from the agent's stats map (telemt's control API, which lists
// every user) and only fall back to the Prometheus series, whose per-user cardinality telemt
// caps. A profile telemt does not mention at all - a key created seconds ago, say - gets no
// row: a zero row would show up in the drawer as a real reading of zero traffic.
func (s *Stats) keySnapshots(ctx context.Context, nodeID uuid.UUID, m TelemtMetrics, stats map[string]string) error {
	profiles, err := s.st.Q.ListNodeProfilesWithKey(ctx, nodeID)
	if err != nil {
		return err
	}
	for _, p := range profiles {
		if !p.AccessKeyID.Valid {
			continue // the node's own service user has no key to attribute traffic to
		}
		conns, haveConns := stats["user."+p.Name+".connections"]
		octets, haveOctets := stats["user."+p.Name+".octets"]
		user, inMetrics := m.Users[p.Name]
		if !haveConns && !haveOctets && !inMetrics {
			continue
		}
		row := db.InsertKeyStatsSnapshotParams{
			AccessKeyID: p.AccessKeyID.UUID, NodeID: nodeID,
			Connections: int32(parseIntOr(conns, int64(user.Connections))),
			TotalOctets: parseIntOr(octets, 0),
			ActiveIps:   int32(parseIntOr(stats["user."+p.Name+".active_ips"], int64(user.UniqueIPs))),
		}
		// quota_used_bytes only means something against a quota; without one it stays 0 so a
		// reader cannot mistake cumulative traffic for consumption of a limit.
		var limits domain.TelemtLimits
		if len(p.KeyTelemtLimits) > 0 {
			_ = json.Unmarshal(p.KeyTelemtLimits, &limits)
		}
		if limits.DataQuotaBytes > 0 {
			row.QuotaUsedBytes = row.TotalOctets
		}
		if err := s.st.Q.InsertKeyStatsSnapshot(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

// parseIntOr reads a stats-map value, falling back to the metrics reading when the map has no
// entry (or an unparseable one) for it.
func parseIntOr(v string, fallback int64) int64 {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func (s *Stats) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.RunOnce(ctx); err != nil {
			s.log.Error("stats", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
