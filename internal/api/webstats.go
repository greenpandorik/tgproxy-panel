package api

import (
	"net/http"
	"time"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
)

// names renders a list of resource names. An empty list is an empty JSON array, never null:
// "nothing is saturated" is an answer, and the UI reads these as arrays.
func names(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// webRuntimeJSON projects the WEB runtime state of the last heartbeat. It returns nil - a JSON
// null - when the node reported none, which is "not available" and not a stopped runtime.
func webRuntimeJSON(w *nodedriver.WebTelemetry) map[string]any {
	if w == nil || w.Runtime == nil {
		return nil
	}
	r := w.Runtime
	out := map[string]any{
		"runtime_instance": r.RuntimeInstance, "carrier_negotiation": r.CarrierNegotiation,
		"learning": nil, "lifecycle": nil, "capacity": nil,
	}
	if l := r.Learning; l != nil {
		out["learning"] = map[string]any{
			"enabled": l.Enabled, "entries": l.Entries, "capacity": l.Capacity,
			"policy_generation": l.PolicyGeneration, "epoch": l.Epoch,
			"lifetime_secs": l.LifetimeSecs, "health_secs": l.HealthSecs, "age_ms": l.AgeMs,
		}
	}
	if lc := r.Lifecycle; lc != nil {
		m := map[string]any{
			"state": lc.State, "epoch": lc.Epoch, "age_ms": lc.AgeMs,
			"admission_open": lc.AdmissionOpen, "effective_new_work_admission": lc.EffectiveNewWorkAdmission,
			"drain": nil,
		}
		if d := lc.Drain; d != nil {
			m["drain"] = map[string]any{
				"operation_id": d.OperationID, "state": d.State, "outcome": d.Outcome,
				"timeout_secs": d.TimeoutSecs, "started_epoch_millis": d.StartedEpochMillis,
				"deadline_epoch_millis": d.DeadlineEpochMillis,
				"remaining_sessions":    d.RemainingSessions, "remaining_streams": d.RemainingStreams,
				"remaining_websockets": d.RemainingWebsockets, "force_close_signalled": d.ForceCloseSignalled,
			}
		}
		out["lifecycle"] = m
	}
	if c := r.Capacity; c != nil {
		resources := make([]map[string]any, 0, len(c.Resources))
		for _, resource := range c.Resources {
			resources = append(resources, map[string]any{
				"resource": resource.Resource, "unit": resource.Unit, "used": resource.Used,
				"available": resource.Available, "limit": resource.Limit, "closed": resource.Closed,
			})
		}
		outcomes := map[string]float64{}
		if c.OverloadOutcomes != nil {
			for _, sample := range c.OverloadOutcomes.Samples {
				outcomes[sample.Label] = sample.Value
			}
		}
		out["capacity"] = map[string]any{
			"connection_capacity_action":    c.ConnectionCapacityAction,
			"max_http_overload_connections": c.MaxHTTPOverloadConnections,
			"http_overload_timeout_ms":      c.HTTPOverloadTimeoutMs,
			"resources":                     resources, "saturated_resources": names(c.SaturatedResources),
			"partial": names(c.Partial), "overload_outcomes": outcomes,
		}
	}
	return out
}

// webCarrierShareJSON is one carrier's slice of the selection distribution. Selections and
// Share are null for a carrier the nodes never reported: the distribution is over the carriers
// telemt actually counted, and a missing carrier is not one with no selections.
type webCarrierShareJSON struct {
	Carrier    string   `json:"carrier"`
	Selections *int64   `json:"selections"`
	Share      *float64 `json:"share"`
}

// webCountersJSON is the WEB counter answer for one window. Every counter is a pointer: null
// means the metric family was not reported and nothing was measured, which is never the same
// as a measured zero.
type webCountersJSON struct {
	From              time.Time             `json:"from"`
	To                time.Time             `json:"to"`
	Samples           int64                 `json:"samples"`
	CounterResets     int64                 `json:"counter_resets"`
	CarrierSelections *int64                `json:"carrier_selections"`
	CarrierFailures   *int64                `json:"carrier_failures"`
	RejectedAttempts  *int64                `json:"rejected_attempts"`
	EvictedSessions   *int64                `json:"evicted_sessions"`
	BridgeRecoveries  *int64                `json:"bridge_recoveries"`
	LearningEntries   *int64                `json:"learning_entries"`
	Distribution      []webCarrierShareJSON `json:"carrier_selection_distribution"`
}

// webDelta is one counter over the window: how much it advanced, and over how many consecutive
// pairs of readings. Zero pairs is absence, whatever the delta says.
type webDelta struct {
	pairs int64
	delta int64
}

func (d webDelta) value() *int64 {
	if d.pairs == 0 {
		return nil
	}
	v := d.delta
	return &v
}

// webWindow is the engine-agnostic shape both window queries reduce to.
type webWindow struct {
	selections map[domain.Carrier]webDelta
	failures   webDelta
	rejections webDelta
	closures   webDelta
	bridge     webDelta
	resets     int64
	samples    int64
}

// distributionCarriers is the fixed set the snapshot columns cover, in the order the panel
// shows them.
var distributionCarriers = []domain.Carrier{
	domain.CarrierWebSocketLanes, domain.CarrierWebSocket,
	domain.CarrierHTTPSLanes, domain.CarrierHTTPS,
}

func webWindowFromRow(r db.WebCounterWindowRow) webWindow {
	return webWindow{
		selections: map[domain.Carrier]webDelta{
			domain.CarrierHTTPS:          {r.SelectionsHttpsPairs, r.SelectionsHttpsDelta},
			domain.CarrierHTTPSLanes:     {r.SelectionsHttpsLanesPairs, r.SelectionsHttpsLanesDelta},
			domain.CarrierWebSocket:      {r.SelectionsWebsocketPairs, r.SelectionsWebsocketDelta},
			domain.CarrierWebSocketLanes: {r.SelectionsWebsocketLanesPairs, r.SelectionsWebsocketLanesDelta},
		},
		failures:   webDelta{r.FailuresPairs, r.FailuresDelta},
		rejections: webDelta{r.RejectionsPairs, r.RejectionsDelta},
		closures:   webDelta{r.ClosuresPairs, r.ClosuresDelta},
		bridge:     webDelta{r.BridgeRecoveriesPairs, r.BridgeRecoveriesDelta},
		resets:     r.CounterResets,
		samples:    r.Samples,
	}
}

func webWindowFromFleetRow(r db.WebCounterWindowAllNodesRow) webWindow {
	return webWindowFromRow(db.WebCounterWindowRow(r))
}

// webCountersResponse turns one window into the JSON body, deriving the selection shares from
// the cumulative counters' deltas.
func webCountersResponse(from, to time.Time, w webWindow, entries webDelta) webCountersJSON {
	out := webCountersJSON{
		From: from, To: to, Samples: w.samples, CounterResets: w.resets,
		CarrierFailures:  w.failures.value(),
		RejectedAttempts: w.rejections.value(),
		EvictedSessions:  w.closures.value(),
		BridgeRecoveries: w.bridge.value(),
		LearningEntries:  entries.value(),
		Distribution:     make([]webCarrierShareJSON, 0, len(distributionCarriers)),
	}
	var total int64
	reported := false
	for _, c := range distributionCarriers {
		if v := w.selections[c].value(); v != nil {
			total += *v
			reported = true
		}
	}
	if reported {
		sum := total
		out.CarrierSelections = &sum
	}
	for _, c := range distributionCarriers {
		row := webCarrierShareJSON{Carrier: string(c), Selections: w.selections[c].value()}
		if row.Selections != nil && total > 0 {
			share := float64(*row.Selections) / float64(total)
			row.Share = &share
		}
		out.Distribution = append(out.Distribution, row)
	}
	return out
}

func (s *Server) handleNodeWebCarriers(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	from, to, ok := parseFromTo(w, r)
	if !ok {
		return
	}
	row, err := s.store.Q.WebCounterWindow(r.Context(), db.WebCounterWindowParams{NodeID: n.ID, FromAt: from, ToAt: to})
	if err != nil {
		internal(w)
		return
	}
	entries, err := s.store.Q.WebLearningEntries(r.Context(), db.WebLearningEntriesParams{NodeID: n.ID, FromAt: from, ToAt: to})
	if err != nil {
		internal(w)
		return
	}
	writeJSON(w, 200, webCountersResponse(from, to, webWindowFromRow(row), webDelta{entries.Nodes, entries.Entries}))
}

func (s *Server) handleFleetWebCarriers(w http.ResponseWriter, r *http.Request) {
	from, to, ok := parseFromTo(w, r)
	if !ok {
		return
	}
	row, err := s.store.Q.WebCounterWindowAllNodes(r.Context(), db.WebCounterWindowAllNodesParams{FromAt: from, ToAt: to})
	if err != nil {
		internal(w)
		return
	}
	entries, err := s.store.Q.WebLearningEntriesAllNodes(r.Context(), db.WebLearningEntriesAllNodesParams{FromAt: from, ToAt: to})
	if err != nil {
		internal(w)
		return
	}
	writeJSON(w, 200, webCountersResponse(from, to, webWindowFromFleetRow(row), webDelta{entries.Nodes, entries.Entries}))
}
