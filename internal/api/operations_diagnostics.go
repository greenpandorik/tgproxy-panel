package api

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"tgwebproxy/internal/alerttext"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodediag"
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
		var previous []domain.DiagnosticGroup
		if rows, e := s.store.Q.ListNodeDiagnostics(ctx, db.ListNodeDiagnosticsParams{NodeID: n.ID, Limit: 1}); e == nil && len(rows) == 1 {
			_ = json.Unmarshal(rows[0].Checks, &previous)
		}
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
				incident := alerttext.Incident{Kind: kind}
				if check.Value != nil {
					incident.Value = *check.Value
				}
				if failed && nodediag.LifetimeCounter(check.Key) {
					growth, grew := counterGrowth(previous, g.Key, check)
					failed = grew
					if grew {
						incident.Growth = strconv.FormatFloat(growth, 'f', -1, 64)
						message += " (+" + incident.Growth + " since the previous check)"
					}
				}
				incident.Message = message
				if failed {
					tag, e := s.store.Pool.Exec(ctx, `INSERT INTO alerts(node_id,kind,message) SELECT $1,$2,$3 WHERE NOT EXISTS(SELECT 1 FROM alerts WHERE node_id=$1 AND kind=$2 AND resolved_at IS NULL)`, n.ID, kind, message)
					if e == nil && tag.RowsAffected() > 0 {
						notifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
						alerts.Incident(notifyCtx, n, incident)
						cancel()
					}
				} else {
					count, e := s.store.Q.ResolveNodeAlerts(ctx, db.ResolveNodeAlertsParams{NodeID: uuid.NullUUID{UUID: n.ID, Valid: true}, Kind: kind})
					if e == nil && count > 0 {
						notifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
						incident.Recovered = true
						alerts.Incident(notifyCtx, n, incident)
						cancel()
					}
				}
			}
		}
		return
	}
}

// counterGrowth is how much a lifetime counter rose since the previous run.
func counterGrowth(previous []domain.DiagnosticGroup, group string, check domain.DiagnosticCheck) (float64, bool) {
	if check.Value == nil {
		return 0, false
	}
	now, err := strconv.ParseFloat(*check.Value, 64)
	if err != nil {
		return 0, false
	}
	for _, g := range previous {
		if g.Key != group {
			continue
		}
		for _, c := range g.Checks {
			if c.Key != check.Key || c.Value == nil {
				continue
			}
			before, err := strconv.ParseFloat(*c.Value, 64)
			if err != nil || now <= before {
				return 0, false
			}
			return now - before, true
		}
	}
	return 0, false
}
