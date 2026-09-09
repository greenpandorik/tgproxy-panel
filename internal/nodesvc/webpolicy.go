package nodesvc

import (
	"encoding/json"

	"tgwebproxy/internal/domain"
)

// WebPolicyOf returns the WEB policy a node should be running: the panel default with
// whatever the operator overrode on top. An empty or unreadable column is the default,
// so no existing node has to be backfilled.
func WebPolicyOf(raw []byte) domain.WebPolicy {
	out := domain.DefaultWebPolicy()
	if len(raw) == 0 {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return domain.DefaultWebPolicy()
	}
	return out
}

// WebPolicyOverrides encodes only what p states differently from the default, so a node
// nobody has touched keeps an empty column and follows the default as the default moves.
func WebPolicyOverrides(p domain.WebPolicy) ([]byte, error) {
	d := domain.DefaultWebPolicy()
	out := map[string]any{}
	if p.Preset != d.Preset {
		out["preset"] = p.Preset
	}
	if p.Carrier != d.Carrier {
		out["carrier"] = p.Carrier
	}
	if !sameCarriers(p.Carriers, d.Carriers) {
		out["carriers"] = p.Carriers
	}
	if p.CarrierLearning != d.CarrierLearning {
		out["carrier_learning"] = p.CarrierLearning
	}
	if p.Aggressiveness != d.Aggressiveness {
		out["carrier_negotiation_aggressiveness"] = p.Aggressiveness
	}
	timeouts := map[string]any{}
	if !sameInts(p.Timeouts.NegotiationDeadlinesSecs, d.Timeouts.NegotiationDeadlinesSecs) {
		timeouts["carrier_negotiation_deadlines_secs"] = p.Timeouts.NegotiationDeadlinesSecs
	}
	for _, f := range []struct {
		name     string
		got, def int
	}{
		{"carrier_health_secs", p.Timeouts.CarrierHealthSecs, d.Timeouts.CarrierHealthSecs},
		{"carrier_learning_secs", p.Timeouts.CarrierLearningSecs, d.Timeouts.CarrierLearningSecs},
		{"bridge_request_secs", p.Timeouts.BridgeRequestSecs, d.Timeouts.BridgeRequestSecs},
		{"bridge_retry_secs", p.Timeouts.BridgeRetrySecs, d.Timeouts.BridgeRetrySecs},
		{"carrier_probe_coalesce_ms", p.Timeouts.ProbeCoalesceMs, d.Timeouts.ProbeCoalesceMs},
	} {
		if f.got != f.def {
			timeouts[f.name] = f.got
		}
	}
	if len(timeouts) > 0 {
		out["timeouts"] = timeouts
	}
	return json.Marshal(out)
}

func sameCarriers(a, b []domain.Carrier) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
