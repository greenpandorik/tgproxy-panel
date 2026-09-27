package api

import (
	"encoding/json"
	"net/http"

	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/reliability"
)

func (s *Server) handleNodeReliability(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	d, ok := s.driver.(nodedriver.ReliabilityDriver)
	if !ok {
		writeError(w, 409, "unsupported", "this driver does not support node reliability", nil)
		return
	}
	var raw json.RawMessage
	if r.Method == http.MethodPut {
		var p reliability.Policy
		if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&p); e != nil {
			validation(w, map[string]string{"body": "invalid policy"})
			return
		}
		if e := p.Validate(); e != nil {
			validation(w, map[string]string{"policy": e.Error()})
			return
		}
		if n.AdTag != "" && (p.Egress == "socks5" || p.AutomaticFailover) {
			conflict(w, "SOCKS/WARP cannot be combined with a sponsor tag")
			return
		}
		raw, _ = json.Marshal(p)
	}
	out, e := d.Reliability(r.Context(), n.ID, raw)
	if e != nil {
		s.driverErr(w, e)
		return
	}
	if r.Method == http.MethodPut {
		s.Audit(r.Context(), "node.reliability", "node", n.ID.String(), nil)
	}
	writeJSON(w, 200, out)
}
