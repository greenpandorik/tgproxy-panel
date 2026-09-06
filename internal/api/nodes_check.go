package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"tgwebproxy/internal/nodecheck"
	"tgwebproxy/internal/store/db"
)

// nodeCheckTimeout bounds the whole prerequisite check (every probe),
// so a POST /nodes/{id}/check request can never hang past a fixed budget.
const nodeCheckTimeout = 15 * time.Second

// checker returns the Checker used for prerequisite probes; tests inject
// s.nodeChecker via apitest options, production leaves it nil and gets the
// real network stack (net.DefaultResolver + net.Dialer, defaulted inside
// Checker itself).
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

	// A telemt node also gets the `mask` probe: a Fake-TLS handshake on classic_port with the
	// node's own tls_domain as SNI, which telemt answers by masking to tls_domain:443. It is
	// the only check that observes the node reaching its own public address. tproxy nodes have
	// no such listener and get the five-probe report unchanged.
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
