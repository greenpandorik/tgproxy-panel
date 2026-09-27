package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/reliability"
)

func (s *Server) handleProbeReport(w http.ResponseWriter, r *http.Request) {
	expected := sha256.Sum256([]byte(s.cfg.ProbeToken))
	provided := sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
	if s.cfg.ProbeToken == "" || subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
		writeError(w, 401, "unauthorized", "invalid probe token", nil)
		return
	}
	if !s.subLimiter.Allow(ipFrom(r.Context())) {
		writeError(w, 429, "rate_limited", "too many reports", nil)
		return
	}
	var p reliability.ProbeReport
	if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&p); e != nil {
		validation(w, map[string]string{"body": "invalid report"})
		return
	}
	if e := p.Validate(time.Now()); e != nil {
		validation(w, map[string]string{"report": e.Error()})
		return
	}
	if !slices.Contains(s.cfg.ProbeLocations, p.Location) {
		validation(w, map[string]string{"location": "location is not in PROBE_LOCATIONS"})
		return
	}
	id, e := uuid.Parse(p.NodeID)
	if e != nil {
		notFound(w)
		return
	}
	if _, e = s.store.Q.GetNode(r.Context(), id); e != nil {
		notFound(w)
		return
	}
	b, _ := json.Marshal(p)
	_, e = s.store.Pool.Exec(r.Context(), `INSERT INTO probe_reports(node_id,location,measured_at,report) VALUES($1,$2,$3,$4) ON CONFLICT(node_id,location) DO UPDATE SET measured_at=EXCLUDED.measured_at,report=EXCLUDED.report WHERE EXCLUDED.measured_at>probe_reports.measured_at`, id, p.Location, p.At, b)
	if e != nil {
		internal(w)
		return
	}
	writeJSON(w, 202, map[string]bool{"accepted": true})
}

func (s *Server) handleNodeProbes(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	rows, e := s.store.Pool.Query(r.Context(), "SELECT report FROM probe_reports WHERE node_id=$1 ORDER BY location", n.ID)
	if e != nil {
		internal(w)
		return
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var b json.RawMessage
		if e = rows.Scan(&b); e != nil {
			internal(w)
			return
		}
		items = append(items, b)
	}
	if rows.Err() != nil {
		internal(w)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "expected_locations": s.cfg.ProbeLocations})
}
