package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"tgwebproxy/internal/nodecheck"
	"tgwebproxy/internal/store/db"
)

const nodeCheckTimeout = 15 * time.Second

func (s *Server) checker() *nodecheck.Checker {
	if s.nodeChecker != nil {
		return s.nodeChecker
	}
	return &nodecheck.Checker{}
}

func (s *Server) handleNodeCheck(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), nodeCheckTimeout)
	defer cancel()

	tlsDomain, classicPort := "", 0
	if n.Engine == db.NodeEngineTelemt {
		tlsDomain, classicPort = n.TlsDomain, int(n.ClassicPort)
	}
	report := s.checker().RunTelemt(ctx, n.Hostname, n.PublicIp, tlsDomain, classicPort)

	raw, err := json.Marshal(report)
	if err != nil {
		internal(w)
		return
	}
	if err := s.store.Q.SetNodeLastCheck(r.Context(), db.SetNodeLastCheckParams{ID: n.ID, LastCheck: raw}); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.check", "node", n.ID.String(), map[string]any{"all_ok": report.AllOK})
	writeJSON(w, 200, report)
}
