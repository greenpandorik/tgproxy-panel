package nodedriver

import (
	"sort"

	agentv1 "tgwebproxy/proto/agent/v1"
)

// WebTelemetryFromProto converts the heartbeat's WEB section. A nil message stays nil, so an
// agent that reports nothing leaves the panel with absence rather than zeros.
func WebTelemetryFromProto(w *agentv1.WebTelemetry) *WebTelemetry {
	if w == nil {
		return nil
	}
	return &WebTelemetry{
		CarrierSelections: webFamilyFromProto(w.GetCarrierSelections()),
		CarrierFailures:   webFamilyFromProto(w.GetCarrierFailures()),
		LearningOutcomes:  webFamilyFromProto(w.GetLearningOutcomes()),
		LearningEntries:   webFamilyFromProto(w.GetLearningEntries()),
		Rejections:        webFamilyFromProto(w.GetRejections()),
		SessionClosures:   webFamilyFromProto(w.GetSessionClosures()),
		BridgeRecovery:    webFamilyFromProto(w.GetBridgeRecovery()),
		Runtime:           webRuntimeFromProto(w.GetRuntime()),
	}
}

// WebTelemetryToProto is the inverse of WebTelemetryFromProto.
func WebTelemetryToProto(w *WebTelemetry) *agentv1.WebTelemetry {
	if w == nil {
		return nil
	}
	return &agentv1.WebTelemetry{
		CarrierSelections: webFamilyToProto(w.CarrierSelections),
		CarrierFailures:   webFamilyToProto(w.CarrierFailures),
		LearningOutcomes:  webFamilyToProto(w.LearningOutcomes),
		LearningEntries:   webFamilyToProto(w.LearningEntries),
		Rejections:        webFamilyToProto(w.Rejections),
		SessionClosures:   webFamilyToProto(w.SessionClosures),
		BridgeRecovery:    webFamilyToProto(w.BridgeRecovery),
		Runtime:           webRuntimeToProto(w.Runtime),
	}
}

func webFamilyFromProto(f *agentv1.WebCounterFamily) *WebCounterFamily {
	if f == nil {
		return nil
	}
	out := &WebCounterFamily{Samples: make([]WebCounterSample, 0, len(f.GetSamples()))}
	for _, s := range f.GetSamples() {
		out.Samples = append(out.Samples, WebCounterSample{
			Carrier: s.GetCarrier(), Label: s.GetLabel(), Phase: s.GetPhase(), Value: s.GetValue(),
		})
	}
	return out
}

func webFamilyToProto(f *WebCounterFamily) *agentv1.WebCounterFamily {
	if f == nil {
		return nil
	}
	out := &agentv1.WebCounterFamily{}
	for _, s := range f.Samples {
		out.Samples = append(out.Samples, &agentv1.WebCounterSample{
			Carrier: s.Carrier, Label: s.Label, Phase: s.Phase, Value: s.Value,
		})
	}
	return out
}

func webRuntimeFromProto(r *agentv1.WebRuntimeState) *WebRuntimeState {
	if r == nil {
		return nil
	}
	out := &WebRuntimeState{RuntimeInstance: r.GetRuntimeInstance(), CarrierNegotiation: r.GetCarrierNegotiation()}
	if l := r.GetLearning(); l != nil {
		out.Learning = &WebLearningState{
			Enabled: l.GetEnabled(), PolicyGeneration: l.GetPolicyGeneration(), Epoch: l.GetEpoch(),
			Entries: int(l.GetEntries()), Capacity: int(l.GetCapacity()),
			LifetimeSecs: l.GetLifetimeSecs(), HealthSecs: l.GetHealthSecs(), AgeMs: l.GetAgeMs(),
		}
	}
	if lc := r.GetLifecycle(); lc != nil {
		out.Lifecycle = &WebLifecycleState{
			State: lc.GetState(), Epoch: lc.GetEpoch(), AgeMs: lc.GetAgeMs(),
			AdmissionOpen: lc.GetAdmissionOpen(), EffectiveNewWorkAdmission: lc.GetEffectiveNewWorkAdmission(),
		}
		if d := lc.GetDrain(); d != nil {
			out.Lifecycle.Drain = &WebDrainState{
				OperationID: d.GetOperationId(), State: d.GetState(), Outcome: d.GetOutcome(),
				TimeoutSecs: int(d.GetTimeoutSecs()), StartedEpochMillis: d.GetStartedEpochMillis(),
				DeadlineEpochMillis: d.GetDeadlineEpochMillis(), RemainingSessions: d.GetRemainingSessions(),
				RemainingStreams: d.GetRemainingStreams(), RemainingWebsockets: d.GetRemainingWebsockets(),
				ForceCloseSignalled: d.GetForceCloseSignalled(),
			}
		}
	}
	if c := r.GetCapacity(); c != nil {
		out.Capacity = &WebCapacityState{
			ConnectionCapacityAction:   c.GetConnectionCapacityAction(),
			MaxHTTPOverloadConnections: c.GetMaxHttpOverloadConnections(),
			HTTPOverloadTimeoutMs:      c.GetHttpOverloadTimeoutMs(),
			SaturatedResources:         append([]string(nil), c.GetSaturatedResources()...),
			Partial:                    append([]string(nil), c.GetPartial()...),
			OverloadOutcomes:           webFamilyFromProto(c.GetOverloadOutcomes()),
		}
		for _, resource := range c.GetResources() {
			out.Capacity.Resources = append(out.Capacity.Resources, WebCapacityResource{
				Resource: resource.GetResource(), Unit: resource.GetUnit(), Used: resource.GetUsed(),
				Available: resource.GetAvailable(), Limit: resource.GetLimit(), Closed: resource.GetClosed(),
			})
		}
	}
	return out
}

func webRuntimeToProto(r *WebRuntimeState) *agentv1.WebRuntimeState {
	if r == nil {
		return nil
	}
	out := &agentv1.WebRuntimeState{RuntimeInstance: r.RuntimeInstance, CarrierNegotiation: r.CarrierNegotiation}
	if l := r.Learning; l != nil {
		out.Learning = &agentv1.WebLearningState{
			Enabled: l.Enabled, PolicyGeneration: l.PolicyGeneration, Epoch: l.Epoch,
			Entries: int32(l.Entries), Capacity: int32(l.Capacity),
			LifetimeSecs: l.LifetimeSecs, HealthSecs: l.HealthSecs, AgeMs: l.AgeMs,
		}
	}
	if lc := r.Lifecycle; lc != nil {
		out.Lifecycle = &agentv1.WebLifecycleState{
			State: lc.State, Epoch: lc.Epoch, AgeMs: lc.AgeMs,
			AdmissionOpen: lc.AdmissionOpen, EffectiveNewWorkAdmission: lc.EffectiveNewWorkAdmission,
		}
		if d := lc.Drain; d != nil {
			out.Lifecycle.Drain = &agentv1.WebDrainState{
				OperationId: d.OperationID, State: d.State, Outcome: d.Outcome,
				TimeoutSecs: int32(d.TimeoutSecs), StartedEpochMillis: d.StartedEpochMillis,
				DeadlineEpochMillis: d.DeadlineEpochMillis, RemainingSessions: d.RemainingSessions,
				RemainingStreams: d.RemainingStreams, RemainingWebsockets: d.RemainingWebsockets,
				ForceCloseSignalled: d.ForceCloseSignalled,
			}
		}
	}
	if c := r.Capacity; c != nil {
		out.Capacity = &agentv1.WebCapacityState{
			ConnectionCapacityAction:   c.ConnectionCapacityAction,
			MaxHttpOverloadConnections: c.MaxHTTPOverloadConnections,
			HttpOverloadTimeoutMs:      c.HTTPOverloadTimeoutMs,
			SaturatedResources:         append([]string(nil), c.SaturatedResources...),
			Partial:                    append([]string(nil), c.Partial...),
			OverloadOutcomes:           webFamilyToProto(c.OverloadOutcomes),
		}
		for _, resource := range c.Resources {
			out.Capacity.Resources = append(out.Capacity.Resources, &agentv1.WebCapacityResource{
				Resource: resource.Resource, Unit: resource.Unit, Used: resource.Used,
				Available: resource.Available, Limit: resource.Limit, Closed: resource.Closed,
			})
		}
	}
	return out
}

// CapabilitiesFromProto merges the settled capabilities with the ones the probe could not
// determine; an undetermined capability is carried as a nil value, never as false.
func CapabilitiesFromProto(c *agentv1.TelemtCapabilities) TelemtCapabilities {
	if c == nil {
		return nil
	}
	out := make(TelemtCapabilities, len(c.GetSupported())+len(c.GetUndetermined()))
	for name, v := range c.GetSupported() {
		value := v
		out[name] = &value
	}
	for _, name := range c.GetUndetermined() {
		out[name] = nil
	}
	return out
}

// CapabilitiesToProto is the inverse of CapabilitiesFromProto.
func CapabilitiesToProto(c TelemtCapabilities, build string) *agentv1.TelemtCapabilities {
	if c == nil {
		return nil
	}
	out := &agentv1.TelemtCapabilities{Supported: map[string]bool{}, Build: build}
	for name, v := range c {
		if v == nil {
			out.Undetermined = append(out.Undetermined, name)
			continue
		}
		out.Supported[name] = *v
	}
	sort.Strings(out.Undetermined)
	return out
}
