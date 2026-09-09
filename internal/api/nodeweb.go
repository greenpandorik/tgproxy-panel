package api

import (
	"net/http"
	"reflect"
	"strings"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodesvc"
	"tgwebproxy/internal/store/db"
)

type webPolicyResp struct {
	Policy     domain.WebPolicy `json:"policy"`
	Default    domain.WebPolicy `json:"default"`
	Overridden bool             `json:"overridden"`
}

func webPolicyJSON(n db.Node) webPolicyResp {
	return webPolicyResp{
		Policy:     nodesvc.WebPolicyOf(n.TelemtWebPolicy),
		Default:    domain.DefaultWebPolicy(),
		Overridden: len(n.TelemtWebPolicy) > 0 && string(n.TelemtWebPolicy) != "{}",
	}
}

func (s *Server) handleGetNodeWebPolicy(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, webPolicyJSON(n))
}

// webPolicyFields turns a domain validation error into the field detail the panel shows.
func webPolicyFields(err error) map[string]string {
	field, msg, found := strings.Cut(err.Error(), ": ")
	if !found || strings.ContainsAny(field, " \"") {
		return map[string]string{"policy": err.Error()}
	}
	return map[string]string{field: msg}
}

func (s *Server) handlePutNodeWebPolicy(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	if n.Engine != db.NodeEngineTelemt {
		validation(w, map[string]string{"engine": "the WEB policy applies to telemt nodes only"})
		return
	}
	current := nodesvc.WebPolicyOf(n.TelemtWebPolicy)
	policy := current
	if err := decodeJSON(r, &policy); err != nil {
		badRequest(w, err.Error())
		return
	}
	if policy.Preset == "" {
		policy.Preset = domain.PresetCustom
	}
	if err := policy.Validate(); err != nil {
		validation(w, webPolicyFields(err))
		return
	}
	if reflect.DeepEqual(policy, current) {
		writeJSON(w, 200, webPolicyJSON(n))
		return
	}
	raw, err := nodesvc.WebPolicyOverrides(policy)
	if err != nil {
		internal(w)
		return
	}
	updated, err := s.store.Q.SetNodeWebPolicy(r.Context(), db.SetNodeWebPolicyParams{ID: n.ID, TelemtWebPolicy: raw})
	if err != nil {
		internal(w)
		return
	}
	if err := s.store.Q.SetNodeDirty(r.Context(), db.SetNodeDirtyParams{ID: n.ID, Dirty: true}); err != nil {
		s.log.Error("mark node dirty", "err", err)
	}
	s.Audit(r.Context(), "node.web_policy", "node", n.ID.String(), map[string]any{"preset": policy.Preset})
	writeJSON(w, 200, webPolicyJSON(updated))
}
