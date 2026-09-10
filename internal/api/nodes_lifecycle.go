package api

import (
	"encoding/json"
	"net/http"

	"tgwebproxy/internal/store/db"
)

func (s *Server) handleWebLifecycle(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	if n.Engine != db.NodeEngineTelemt {
		conflict(w, "WEB lifecycle is supported on Telemt only")
		return
	}
	var body struct {
		Action      string `json:"action"`
		TimeoutSecs int    `json:"timeout_secs"`
	}
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, err.Error())
		return
	}
	names := map[string]string{"pause": "WebPause", "drain": "WebDrain", "resume": "WebResume", "reset_learning": "CarrierLearningReset"}
	capability, exists := names[body.Action]
	if !exists {
		validation(w, map[string]string{"action": "unknown action"})
		return
	}
	var caps map[string]*bool
	_ = json.Unmarshal(n.TelemtCapabilities, &caps)
	if caps[capability] == nil || !*caps[capability] {
		conflict(w, "capability is unsupported or has not been determined; refresh node health")
		return
	}
	if body.TimeoutSecs == 0 {
		body.TimeoutSecs = 120
	}
	if body.TimeoutSecs < 1 || body.TimeoutSecs > 3600 {
		validation(w, map[string]string{"timeout_secs": "must be 1..3600"})
		return
	}
	if err := s.driver.ControlWeb(r.Context(), n.ID, body.Action, body.TimeoutSecs); err != nil {
		s.driverErr(w, err)
		return
	}
	s.Audit(r.Context(), "node.web_"+body.Action, "node", n.ID.String(), nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
}
