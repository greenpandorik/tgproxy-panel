package worker

import (
	"context"
	"encoding/json"
	"time"

	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/store/db"
)

func (s *Stats) SetProbeLocations(locations []string) {
	s.probeLocations = append([]string(nil), locations...)
}

func (s *Stats) collectProbes(ctx context.Context, n db.Node) {
	if len(s.probeLocations) == 0 {
		return
	}
	var h struct{ Reliability json.RawMessage }
	var health reliability.Report
	if json.Unmarshal(n.LastHealth, &h) == nil && json.Unmarshal(h.Reliability, &health) == nil && health.Policy.Maintenance {
		return
	}
	rows, e := s.st.Pool.Query(ctx, "SELECT report FROM probe_reports WHERE node_id=$1", n.ID)
	if e != nil {
		return
	}
	reports := map[string]reliability.ProbeReport{}
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			rows.Close()
			return
		}
		var p reliability.ProbeReport
		if json.Unmarshal(raw, &p) == nil {
			reports[p.Location] = p
		}
	}
	err := rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	now := s.clock()
	for _, loc := range s.probeLocations {
		p, ok := reports[loc]
		stale := !ok || now.Sub(p.At) > 3*time.Minute
		findings := []reliability.Finding{{Kind: "probe_" + loc + "_stale", Message: "External probe " + loc + " has no fresh report", Known: true, Failed: stale}}
		if !stale {
			for key, c := range map[string]reliability.ProbeCheck{"tls": p.TLS, "http": p.HTTP, "faketls": p.FakeTLS, "web": p.WEB} {
				findings = append(findings, reliability.Finding{Kind: "probe_" + loc + "_" + key, Message: "External probe " + loc + ": " + key + " failed", Known: c.Status != "not_run", Failed: c.Status == "failed"})
			}
		}
		s.recordFindings(ctx, n, findings)
	}
}
