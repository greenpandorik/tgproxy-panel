package worker

import (
	"context"
	"encoding/json"
	"time"

	"tgwebproxy/internal/alerttext"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/store/db"
)

func (s *Stats) collectIncidents(ctx context.Context, n db.Node) {
	var h nodedriver.HealthReport
	if json.Unmarshal(n.LastHealth, &h) != nil {
		return
	}
	var r reliability.Report
	if json.Unmarshal(h.Reliability, &r) != nil || r.Version == 0 || time.Since(r.At) > 2*time.Minute {
		return
	}
	if r.Policy.Maintenance {
		return
	}
	findings := reliability.Findings(r, time.Now())
	findings = append(findings, reliability.Finding{Kind: "disk_pressure", Message: "Server disk is at least 90% full", Failed: h.DiskUsedPercent >= 90, Known: true}, reliability.Finding{Kind: "memory_pressure", Message: "Server memory is at least 95% full", Failed: h.MemUsedPercent >= 95, Known: true}, reliability.Finding{Kind: "engine_unready", Message: "Proxy engine is not ready", Failed: !h.Readyz || !h.Healthz, Known: true})
	if h.Web != nil && h.Web.Runtime != nil && h.Web.Runtime.Lifecycle != nil {
		findings = append(findings, reliability.Finding{Kind: "web_admission_closed", Message: "WEB admission is closed; inspect maintenance or pause state", Failed: !h.Web.Runtime.Lifecycle.AdmissionOpen, Known: true})
	}
	s.recordFindings(ctx, n, findings)
}

// findingStreak is how many sweeps in a row a finding must keep its state before it opens or closes an alert.
const findingStreak = 3

// findingRun records one sweep's state for key and returns how many sweeps in a row it has held.
func (s *Stats) findingRun(key string, failed bool) int {
	s.findingMu.Lock()
	defer s.findingMu.Unlock()
	run := s.findingStreak[key]
	switch {
	case failed && run > 0:
		run++
	case failed:
		run = 1
	case run < 0:
		run--
	default:
		run = -1
	}
	s.findingStreak[key] = run
	if run < 0 {
		return -run
	}
	return run
}

func (s *Stats) recordFindings(ctx context.Context, n db.Node, findings []reliability.Finding) {
	for _, f := range findings {
		if !f.Known {
			continue
		}
		kind := "reliability_" + f.Kind
		if s.findingRun(n.ID.String()+"|"+kind, f.Failed) < findingStreak {
			continue
		}
		if f.Failed {
			// One stats sweep owns a node. Persisted incidents survive process restarts.
			tag, e := s.st.Pool.Exec(ctx, `INSERT INTO alerts(node_id,kind,message) SELECT $1,$2,$3 WHERE NOT EXISTS(SELECT 1 FROM alerts WHERE node_id=$1 AND kind=$2 AND resolved_at IS NULL)`, n.ID, kind, f.Message)
			if e == nil && tag.RowsAffected() > 0 {
				s.notify(context.WithoutCancel(ctx), func(ctx context.Context) {
					s.alerts.Incident(ctx, n, alerttext.Incident{Kind: kind, Message: f.Message})
				})
			}
		} else {
			rows, e := s.st.Q.ResolveNodeAlerts(ctx, db.ResolveNodeAlertsParams{NodeID: nullUUID(n.ID), Kind: kind})
			if e == nil && rows > 0 {
				s.notify(context.WithoutCancel(ctx), func(ctx context.Context) {
					s.alerts.Incident(ctx, n, alerttext.Incident{Kind: kind, Message: f.Message, Recovered: true})
				})
			}
		}
	}
}

func (a *Alerts) Incident(ctx context.Context, n db.Node, in alerttext.Incident) {
	if a == nil {
		return
	}
	msg := alerttext.Default().Incident(a.lang(ctx), textNode(n), in, a.PanelURL)
	if in.Recovered {
		a.recovered(ctx, n.ID.String(), in.Kind, msg)
		return
	}
	a.problem(ctx, n.ID.String(), in.Kind, incidentCooldown, msg)
}
