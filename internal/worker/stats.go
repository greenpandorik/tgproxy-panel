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

// TelemtMetrics is the telemt equivalent of RelayMetrics.
type TelemtMetrics struct {
	ConnectionsTotal int64
	ConnectionsLive  int
	Users            map[string]TelemtUserMetrics
}

// ParseTelemtMetrics reads the telemt_* metrics the panel charts.
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

// splitUserLabel splits `telemt_user_x{user="k1"}` into the metric name and the user.
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
func telemtRelayMetrics(m TelemtMetrics, stats map[string]string) RelayMetrics {
	live := m.ConnectionsLive
	if len(m.Users) == 0 {
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

	notifySlots chan struct{}
	notifyWG    sync.WaitGroup

	lastKeyStatsSweep time.Time
	lastHistorySweep  time.Time
	now               func() time.Time
}

// maxInFlightNotifications bounds the goroutines RunOnce may have out sending Telegram messages.
const maxInFlightNotifications = 32

func NewStats(st *store.Store, driver nodedriver.Driver, offlineAfter time.Duration, log *slog.Logger) *Stats {
	return &Stats{
		st: st, driver: driver, offlineAfter: offlineAfter, log: log,
		notifySlots: make(chan struct{}, maxInFlightNotifications),
	}
}

// notify runs one alert notification off RunOnce's goroutine.
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

// WaitNotifications blocks until every notification spawned so far has finished.
func (s *Stats) WaitNotifications() { s.notifyWG.Wait() }

func (s *Stats) SetOfflineAfterFunc(f func(context.Context) time.Duration) {
	s.offlineAfterFunc = f
}

// SetAlerts installs the Telegram alert notifier; nil (the default) means no notifications are sent.
func (s *Stats) SetAlerts(a *Alerts) { s.alerts = a }

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
		load := nodeLoad(n.LastHealth)
		if err := s.st.Q.InsertSnapshot(ctx, db.InsertSnapshotParams{
			NodeID: n.ID, SessionsLive: int32(m.SessionsLive), StreamsLive: int32(m.StreamsLive),
			BytesUp: m.BytesUp, BytesDown: m.BytesDown, SessionsCreated: m.SessionsCreated, LimitHits: m.LimitHits,
			CpuPercent: load.CpuPercent, MemUsedPercent: load.MemUsedPercent, DiskUsedPercent: load.DiskUsedPercent,
			DcLatency:  load.DcLatency,
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
	if err := s.st.Q.DeleteExpiredSessions(ctx); err != nil {
		s.log.Error("delete expired sessions", "err", err)
	}
	s.sweepKeyStats(ctx)
	s.sweepHistory(ctx)
	return s.st.Q.DeleteOldSnapshots(ctx, time.Now().Add(-retention))
}

// nodeLoad reads the server-load percentages out of a node's last heartbeat.
func nodeLoad(lastHealth []byte) db.InsertSnapshotParams {
	var h nodedriver.HealthReport
	if len(lastHealth) == 0 || json.Unmarshal(lastHealth, &h) != nil {
		return db.InsertSnapshotParams{DcLatency: []byte("{}")}
	}
	return db.InsertSnapshotParams{
		CpuPercent: float32(h.CPUPercent), MemUsedPercent: float32(h.MemUsedPercent), DiskUsedPercent: float32(h.DiskUsedPercent),
		DcLatency: dcLatencyJSON(h),
	}
}

func dcLatencyJSON(h nodedriver.HealthReport) []byte {
	if !h.DcDataAvailable {
		return []byte("{}")
	}
	out := make(map[string]float64, len(h.DCs))
	for _, d := range h.DCs {
		if d.Known {
			out[strconv.Itoa(d.DC)] = d.LatencyMs
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

const (
	keyStatsSweepEvery      = 10 * time.Minute
	keyStatsSweepBatch      = 5000
	keyStatsSweepMaxBatches = 40
)

func (s *Stats) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

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

// retention is how long both snapshot tables are kept.
const retention = 30 * 24 * time.Hour

const (
	historySweepEvery = time.Hour
	// auditRetention is longer than the rest: the audit trail is what an operator reads
	// after an incident, and an incident is not always noticed the same month.
	auditRetention    = 180 * 24 * time.Hour
	applyJobRetention = 30 * 24 * time.Hour
)

// sweepHistory trims the two tables that would otherwise grow for the life of the install:
// the audit trail and the finished apply jobs with their logs.
func (s *Stats) sweepHistory(ctx context.Context) {
	now := s.clock()
	if !s.lastHistorySweep.IsZero() && now.Sub(s.lastHistorySweep) < historySweepEvery {
		return
	}
	s.lastHistorySweep = now
	if n, err := s.st.Q.DeleteOldAudit(ctx, now.Add(-auditRetention)); err != nil {
		s.log.Error("delete old audit entries", "err", err)
	} else if n > 0 {
		s.log.Info("audit retention sweep", "deleted", n)
	}
	cutoff := now.Add(-applyJobRetention)
	if n, err := s.st.Q.DeleteOldApplyJobs(ctx, &cutoff); err != nil {
		s.log.Error("delete old apply jobs", "err", err)
	} else if n > 0 {
		s.log.Info("apply job retention sweep", "deleted", n)
	}
}

// keySnapshots writes one key_stats_snapshots row per key that telemt reported on this node.
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
