package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

// One bounded pass per operations tick; the oldest observation goes first.
func (s *Server) scheduledDiagnostics(ctx context.Context, alerts *worker.Alerts) {
	nodes, e := s.store.Q.ListNodes(ctx)
	if e != nil {
		return
	}
	for _, n := range nodes {
		if !s.driver.Online(n.ID) {
			continue
		}
		var h nodedriver.HealthReport
		var report reliability.Report
		if json.Unmarshal(n.LastHealth, &h) == nil && json.Unmarshal(h.Reliability, &report) == nil && report.Policy.Maintenance {
			continue
		}
		var due bool
		if s.store.Pool.QueryRow(ctx, "SELECT NOT EXISTS(SELECT 1 FROM node_diagnostics WHERE node_id=$1 AND started_at>now()-interval '15 minutes')", n.ID).Scan(&due) != nil || !due {
			continue
		}
		release, e := s.acquireDiag(n.ID)
		if e != nil {
			continue
		}
		run := s.diagEngine().Run(ctx, diagTarget(n), domain.TriggerScheduled)
		release()
		if e = s.saveDiagnostics(ctx, n.ID, &run); e != nil {
			return
		}
		for _, g := range run.Groups {
			for _, check := range g.Checks {
				if check.Status == domain.CheckNotAvailable {
					continue
				}
				kind := "diagnostic_" + g.Key + "_" + check.Key
				failed := check.Status == domain.CheckFail || check.Status == domain.CheckWarn
				message := "Scheduled check: " + g.Key + " / " + check.Key
				if check.Detail != nil {
					message += ": " + *check.Detail
				}
				if failed {
					tag, e := s.store.Pool.Exec(ctx, `INSERT INTO alerts(node_id,kind,message) SELECT $1,$2,$3 WHERE NOT EXISTS(SELECT 1 FROM alerts WHERE node_id=$1 AND kind=$2 AND resolved_at IS NULL)`, n.ID, kind, message)
					if e == nil && tag.RowsAffected() > 0 {
						notifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
						alerts.Incident(notifyCtx, n, kind, message, false)
						cancel()
					}
				} else {
					count, e := s.store.Q.ResolveNodeAlerts(ctx, db.ResolveNodeAlertsParams{NodeID: uuid.NullUUID{UUID: n.ID, Valid: true}, Kind: kind})
					if e == nil && count > 0 {
						notifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
						alerts.Incident(notifyCtx, n, kind, message, true)
						cancel()
					}
				}
			}
		}
		return
	}
}
