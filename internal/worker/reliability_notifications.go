package worker

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"tgwebproxy/internal/alerttext"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/store/db"
)

const (
	reliabilityProblemDelay  = 5 * time.Minute
	reliabilityRecoveryDelay = 10 * time.Minute
	reliabilityFreshness     = 2 * time.Minute
)

// Each complete source snapshot replaces its known findings. Missing previously
// observed DCs/checks become unknown immediately rather than silently recovering.
func (s *Stats) observeFindings(ctx context.Context, n db.Node, findings []reliability.Finding) {
	if s.alerts == nil || len(findings) == 0 {
		return
	}
	source := "health"
	if findings[0].Kind == "looks_like_blocking" {
		source = "blocking"
	} else if strings.HasPrefix(findings[0].Kind, "probe_") {
		source = strings.TrimSuffix(findings[0].Kind, "_stale")
	}
	tx, err := s.st.Pool.Begin(ctx)
	if err != nil {
		s.log.Error("notification observations begin", "err", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `UPDATE notification_findings SET known=false WHERE node_id=$1 AND source=$2`, n.ID, source); err != nil {
		return
	}
	for _, f := range findings {
		if !f.Known {
			if _, err = tx.Exec(ctx, `UPDATE notification_findings SET known=false,observed_at=$3 WHERE node_id=$1 AND kind=$2`, n.ID, f.Kind, s.clock()); err != nil {
				return
			}
			continue
		}
		if _, err = tx.Exec(ctx, `INSERT INTO notification_findings(node_id,source,kind,failed,known,observed_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(node_id,kind) DO UPDATE SET source=excluded.source,failed=excluded.failed,known=excluded.known,observed_at=excluded.observed_at`, n.ID, source, f.Kind, f.Failed, f.Known, s.clock()); err != nil {
			return
		}
	}
	if err = tx.Commit(ctx); err != nil {
		s.log.Error("notification observations save", "err", err)
	}
}

func (s *Stats) notifyReliability(ctx context.Context, n db.Node) {
	if s.alerts == nil {
		return
	}
	now := s.clock()
	known := freshNotificationHealth(n, now)
	// Enqueue the attempt, not a transition: observation/state remains in PostgreSQL
	// when the notification queue is full or delivery fails.
	if !s.alerts.observeReliability(ctx, n, known, now) {
		return
	}
	s.notify(context.WithoutCancel(ctx), func(ctx context.Context) { s.alerts.sendReliability(ctx, n) })
}

func (a *Alerts) observeReliability(ctx context.Context, n db.Node, known bool, now time.Time) bool {
	if a == nil || a.pool == nil {
		return false
	}
	var bad, unknown, any bool
	if err := a.pool.QueryRow(ctx, `SELECT COALESCE(bool_or(failed AND known AND observed_at >= $2),false), COALESCE(bool_or(NOT known OR observed_at < $2),false),count(*)>0 FROM notification_findings WHERE node_id=$1 AND source<>'blocking_evidence'`, n.ID, now.Add(-reliabilityFreshness)).Scan(&bad, &unknown, &any); err != nil {
		a.log.Error("notification findings read", "err", err)
		return false
	}
	var diagnostic bool
	if err := a.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts WHERE node_id=$1 AND resolved_at IS NULL AND kind LIKE 'diagnostic\_%' ESCAPE '\')`, n.ID).Scan(&diagnostic); err != nil {
		return false
	}
	bad = bad || diagnostic
	phase := "unknown"
	if known && bad {
		phase = "bad"
	} else if known && any && !unknown {
		phase = "healthy"
	}
	_, err := a.pool.Exec(ctx, `INSERT INTO notification_candidates(node_id,phase,since,observed_at) VALUES($1,$2,$3,$3) ON CONFLICT(node_id) DO UPDATE SET phase=excluded.phase,since=CASE WHEN notification_candidates.phase != excluded.phase OR notification_candidates.observed_at < $4 OR excluded.phase='unknown' THEN excluded.since ELSE notification_candidates.since END,observed_at=excluded.observed_at`, n.ID, phase, now, now.Add(-reliabilityFreshness))
	if err != nil {
		a.log.Error("notification candidate save", "err", err)
		return false
	}
	return true
}

func (a *Alerts) sendReliability(ctx context.Context, n db.Node) {
	a.withState(ctx, n.ID.String(), "server_reliability", func(state *notificationState) {
		var phase string
		var since, now time.Time
		if err := a.pool.QueryRow(ctx, `SELECT phase,since,observed_at FROM notification_candidates WHERE node_id=$1`, n.ID).Scan(&phase, &since, &now); err != nil {
			return
		}
		deliveryNow := time.Now()
		if a.now != nil {
			deliveryNow = a.now()
		}
		if deliveryNow.Sub(now) > reliabilityFreshness {
			return
		}
		switch phase {
		case "bad":
			if now.Sub(since) < reliabilityProblemDelay {
				return
			}
			// A channel that recovered can open a new episode even when another
			// channel's previous recovery is still awaiting delivery. Existing
			// acknowledgements suppress repeats for unrecovered channels.
			var blocking bool
			if err := a.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notification_findings WHERE node_id=$1 AND source='blocking' AND kind='looks_like_blocking' AND failed AND known AND observed_at >= $2)`, n.ID, now.Add(-reliabilityFreshness)).Scan(&blocking); err != nil {
				return
			}
			state.Pending = true
			a.sendProblem(ctx, n.ID.String(), "server_reliability", alerttext.Default().Reliability(a.lang(ctx), textNode(n), false, a.PanelURL, blocking), state)
		case "healthy":
			if now.Sub(since) < reliabilityRecoveryDelay {
				return
			}
			state.Pending = false
			a.sendRecovery(ctx, n.ID.String(), "server_reliability", alerttext.Default().Reliability(a.lang(ctx), textNode(n), true, a.PanelURL, false), state)
			if !state.Telegram && !state.Webhook {
				state.LastProblem = time.Time{}
			}
		}
	})
}

// Tproxy heartbeats predate the versioned Telemt reliability report. Their
// persisted heartbeat time still establishes fresh health for aggregated checks.
func freshNotificationHealth(n db.Node, now time.Time) bool {
	var h nodedriver.HealthReport
	if json.Unmarshal(n.LastHealth, &h) != nil {
		return false
	}
	var r reliability.Report
	if json.Unmarshal(h.Reliability, &r) == nil && r.Version > 0 {
		return !r.Policy.Maintenance && now.Sub(r.At) <= reliabilityFreshness
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(n.LastHealth, &fields) != nil {
		return false
	}
	_, healthKnown := fields["Healthz"]
	_, readyKnown := fields["Readyz"]
	return healthKnown && readyKnown && n.Engine == db.NodeEngineTproxy && len(h.Reliability) == 0 && n.LastSeenAt != nil && now.Sub(*n.LastSeenAt) <= reliabilityFreshness
}
