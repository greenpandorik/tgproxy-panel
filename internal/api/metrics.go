package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"tgwebproxy/internal/store"
)

var (
	descNodes    = prometheus.NewDesc("tgwp_nodes", "nodes by status", []string{"status"}, nil)
	descKeys     = prometheus.NewDesc("tgwp_keys", "keys by status", []string{"status"}, nil)
	descSessions = prometheus.NewDesc("tgwp_node_sessions_live", "live relay sessions", []string{"node"}, nil)
	descStreams  = prometheus.NewDesc("tgwp_node_streams_live", "live relay streams", []string{"node"}, nil)

	// nodeStatuses and keyStatuses are emitted even when their count is 0 so
	// scrapers always see the full label set.
	nodeStatuses = []string{"pending", "online", "offline", "degraded"}
	keyStatuses  = []string{"pending", "active", "revoked"}
)

// dbCollector refreshes nodes/keys/live-session gauges from the database on
// every scrape.
type dbCollector struct{ st *store.Store }

func (c dbCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descNodes
	ch <- descKeys
	ch <- descSessions
	ch <- descStreams
}

func (c dbCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	nodeCounts := map[string]int64{}
	for _, st := range nodeStatuses {
		nodeCounts[st] = 0
	}
	if rows, err := c.st.Q.CountNodesByStatus(ctx); err == nil {
		for _, row := range rows {
			nodeCounts[string(row.Status)] = row.N
		}
	}
	for _, status := range nodeStatuses {
		ch <- prometheus.MustNewConstMetric(descNodes, prometheus.GaugeValue, float64(nodeCounts[status]), status)
	}

	keyCounts := map[string]int64{}
	for _, st := range keyStatuses {
		keyCounts[st] = 0
	}
	if rows, err := c.st.Q.CountKeysByStatus(ctx); err == nil {
		for _, row := range rows {
			keyCounts[string(row.Status)] = row.N
		}
	}
	for _, status := range keyStatuses {
		ch <- prometheus.MustNewConstMetric(descKeys, prometheus.GaugeValue, float64(keyCounts[status]), status)
	}

	if snaps, err := c.st.Q.LatestSnapshots(ctx); err == nil {
		for _, snap := range snaps {
			node := snap.NodeID.String()
			ch <- prometheus.MustNewConstMetric(descSessions, prometheus.GaugeValue, float64(snap.SessionsLive), node)
			ch <- prometheus.MustNewConstMetric(descStreams, prometheus.GaugeValue, float64(snap.StreamsLive), node)
		}
	}
}

// newMetricsHandler builds a dedicated registry (Go + process collectors
// plus dbCollector) so panel metrics never mix with a default global
// registry.
func newMetricsHandler(st *store.Store) http.Handler {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(dbCollector{st: st})
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

// handleMetrics serves /metrics at the root, public unless MetricsToken is
// configured.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.cfg.MetricsToken != "" {
		auth := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.MetricsToken)) != 1 {
			unauthorized(w)
			return
		}
	}
	s.metricsHandler.ServeHTTP(w, r)
}
