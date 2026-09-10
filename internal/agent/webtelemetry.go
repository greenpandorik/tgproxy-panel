package agent

import (
	"context"
	"sort"
	"time"

	"tgwebproxy/internal/telemt"
	agentv1 "tgwebproxy/proto/agent/v1"
)

const (
	// webCollectTimeout bounds WEB collection so a wedged telemt cannot hold up the
	// heartbeat that carries everything else.
	webCollectTimeout = 5 * time.Second
	// capabilityProbeEvery is how often the agent recomputes the capability set.
	capabilityProbeEvery = 10 * time.Minute
)

// webTelemetry reads the telemt_web_* families and the WEB runtime state. Anything it cannot
// read is left nil: an absent family is not a zeroed one.
func (h *Handler) webTelemetry(ctx context.Context) *agentv1.WebTelemetry {
	if h.tm == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, webCollectTimeout)
	defer cancel()
	out := &agentv1.WebTelemetry{}
	read := false
	if m, err := h.tm.WebMetrics(ctx); err == nil {
		fillWebCounters(out, m)
		read = true
	} else {
		h.log.Debug("web metrics unavailable", "err", err)
	}
	if st, err := h.tm.WebStatus(ctx); err == nil {
		out.Runtime = webRuntimeState(st)
		read = true
	} else {
		h.log.Debug("web status unavailable", "err", err)
	}
	if !read {
		return nil
	}
	return out
}

func fillWebCounters(out *agentv1.WebTelemetry, m telemt.WebMetrics) {
	out.CarrierSelections = carrierFamilyProto(m.CarrierSelections)
	out.LearningOutcomes = carrierFamilyProto(m.LearningOutcomes)
	out.SessionClosures = carrierFamilyProto(m.SessionClosures)
	out.CarrierFailures = failureFamilyProto(m.CarrierFailures)
	out.LearningEntries = labelFamilyProto(m.LearningEntries)
	out.Rejections = labelFamilyProto(m.Rejections)
	out.BridgeRecovery = labelFamilyProto(m.BridgeRecovery)
}

func labelFamilyProto(f telemt.LabelCounters) *agentv1.WebCounterFamily {
	if !f.Present {
		return nil
	}
	out := &agentv1.WebCounterFamily{}
	for _, label := range f.Labels() {
		v, _ := f.Get(label)
		out.Samples = append(out.Samples, &agentv1.WebCounterSample{Label: label, Value: v})
	}
	return out
}

func carrierFamilyProto(f telemt.CarrierCounters) *agentv1.WebCounterFamily {
	if !f.Present {
		return nil
	}
	out := &agentv1.WebCounterFamily{}
	for _, carrier := range f.Carriers() {
		inner := f.Carrier(carrier)
		for _, label := range inner.Labels() {
			v, _ := inner.Get(label)
			out.Samples = append(out.Samples, &agentv1.WebCounterSample{Carrier: carrier, Label: label, Value: v})
		}
	}
	return out
}

func failureFamilyProto(f telemt.CarrierFailures) *agentv1.WebCounterFamily {
	if !f.Present {
		return nil
	}
	out := &agentv1.WebCounterFamily{}
	carriers := make([]string, 0, len(f.Values))
	for carrier := range f.Values {
		carriers = append(carriers, carrier)
	}
	sort.Strings(carriers)
	for _, carrier := range carriers {
		byKey := f.Values[carrier]
		keys := make([]telemt.CarrierFailureKey, 0, len(byKey))
		for k := range byKey {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].Phase != keys[j].Phase {
				return keys[i].Phase < keys[j].Phase
			}
			return keys[i].Reason < keys[j].Reason
		})
		for _, k := range keys {
			out.Samples = append(out.Samples, &agentv1.WebCounterSample{
				Carrier: carrier, Phase: k.Phase, Label: k.Reason, Value: byKey[k],
			})
		}
	}
	return out
}

func webRuntimeState(st telemt.WebStatus) *agentv1.WebRuntimeState {
	out := &agentv1.WebRuntimeState{
		RuntimeInstance:    st.RuntimeInstance,
		CarrierNegotiation: st.HasCarrierNegotiation(),
	}
	if l := st.Runtime.Learning; l != nil {
		out.Learning = &agentv1.WebLearningState{
			Enabled: l.Enabled, PolicyGeneration: l.PolicyGeneration, Epoch: l.Epoch,
			Entries: int32(l.Entries), Capacity: int32(l.Capacity),
			LifetimeSecs: l.LifetimeSecs, HealthSecs: l.HealthSecs, AgeMs: l.AgeMs,
		}
	}
	if lc := st.OperatorLifecycle; lc != nil {
		out.Lifecycle = &agentv1.WebLifecycleState{
			State: lc.State, Epoch: lc.Epoch, AgeMs: lc.AgeMs,
			AdmissionOpen: lc.AdmissionOpen, EffectiveNewWorkAdmission: lc.EffectiveNewWorkAdmission,
		}
		if d := lc.Drain; d != nil {
			out.Lifecycle.Drain = &agentv1.WebDrainState{
				OperationId: d.OperationID.String(), State: d.State, Outcome: d.Outcome,
				TimeoutSecs: int32(d.TimeoutSecs), StartedEpochMillis: d.StartedEpochMillis,
				DeadlineEpochMillis: d.DeadlineEpochMillis, RemainingSessions: d.RemainingSessions,
				RemainingStreams: d.RemainingStreams, RemainingWebsockets: d.RemainingWebsockets,
				ForceCloseSignalled: d.ForceCloseSignalled,
			}
		}
	}
	if capacity := st.Capacity; capacity != nil {
		out.Capacity = &agentv1.WebCapacityState{
			ConnectionCapacityAction:   capacity.HTTPConnectionCapacityAction,
			MaxHttpOverloadConnections: capacity.MaxHTTPOverloadConnections,
			HttpOverloadTimeoutMs:      capacity.HTTPOverloadTimeoutMs,
			SaturatedResources:         append([]string(nil), capacity.SaturatedResources...),
			Partial:                    append([]string(nil), capacity.Partial...),
			OverloadOutcomes:           &agentv1.WebCounterFamily{},
		}
		for _, resource := range capacity.Resources {
			out.Capacity.Resources = append(out.Capacity.Resources, &agentv1.WebCapacityResource{
				Resource: resource.Resource, Unit: resource.Unit, Used: resource.Used,
				Available: resource.Available, Limit: resource.Limit, Closed: resource.Closed,
			})
		}
		for _, outcome := range capacity.HTTPConnectionOverloadOutcomes {
			out.Capacity.OverloadOutcomes.Samples = append(out.Capacity.OverloadOutcomes.Samples,
				&agentv1.WebCounterSample{Label: outcome.Outcome, Value: float64(outcome.Total)})
		}
	}
	return out
}

// telemtCapabilities returns the capability set, recomputed at most every capabilityProbeEvery.
// A probe that fails returns nil so the panel keeps whatever it already knows.
func (h *Handler) telemtCapabilities(ctx context.Context) *agentv1.TelemtCapabilities {
	if h.tm == nil {
		return nil
	}
	h.mu.Lock()
	cached, at := h.caps, h.capsAt
	h.mu.Unlock()
	if cached != nil && time.Since(at) < capabilityProbeEvery {
		return cached
	}
	ctx, cancel := context.WithTimeout(ctx, webCollectTimeout)
	defer cancel()
	caps, unknown, err := h.tm.Capabilities(ctx)
	if err != nil {
		h.log.Debug("capability probe failed", "err", err)
		return nil
	}
	out := capabilitiesToProto(caps, unknown)
	if info, infoErr := h.tm.SystemInfo(ctx); infoErr == nil {
		out.Build = info.BuildID()
	}
	h.mu.Lock()
	h.caps, h.capsAt = out, time.Now()
	h.mu.Unlock()
	return out
}

func capabilitiesToProto(caps telemt.TelemtCapabilities, unknown telemt.UnknownCapabilities) *agentv1.TelemtCapabilities {
	all := map[string]bool{
		telemt.CapWeb:                  caps.Web,
		telemt.CapCarrierNegotiation:   caps.CarrierNegotiation,
		telemt.CapCarrierLearning:      caps.CarrierLearning,
		telemt.CapCarrierLearningReset: caps.CarrierLearningReset,
		telemt.CapWebPause:             caps.WebPause,
		telemt.CapWebDrain:             caps.WebDrain,
		telemt.CapWebResume:            caps.WebResume,
		telemt.CapWebRuntime:           caps.WebRuntime,
		telemt.CapHTTPUpstreamDecoy:    caps.HttpUpstreamDecoy,
		telemt.CapTLSEmulation:         caps.TLSEmulation,
		telemt.CapMiddleProxy:          caps.MiddleProxy,
	}
	out := &agentv1.TelemtCapabilities{Supported: map[string]bool{}, Undetermined: unknown.Names()}
	for name, v := range all {
		if unknown.Has(name) {
			continue
		}
		out.Supported[name] = v
	}
	return out
}
