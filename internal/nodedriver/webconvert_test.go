package nodedriver

import (
	"encoding/json"
	"testing"

	agentv1 "tgwebproxy/proto/agent/v1"
)

func TestHealthWithoutWebSectionRoundTrips(t *testing.T) {
	out := HealthFromProto(&agentv1.HealthReport{RelayActive: true, CpuPercent: 3})
	if out.Web != nil || out.Capabilities != nil || out.TelemtBuild != "" {
		t.Fatalf("an old agent reports no WEB section: %+v", out)
	}
	if back := HealthToProto(out); back.GetWeb() != nil || back.GetCapabilities() != nil {
		t.Fatalf("nil must stay nil through the round trip: %+v", back)
	}
}

func TestWebTelemetryRoundTripKeepsAbsentFamiliesNil(t *testing.T) {
	in := &agentv1.WebTelemetry{
		CarrierSelections: &agentv1.WebCounterFamily{Samples: []*agentv1.WebCounterSample{
			{Carrier: "https", Label: "applied", Value: 7},
		}},
		BridgeRecovery: &agentv1.WebCounterFamily{},
	}
	got := WebTelemetryFromProto(in)
	if v, ok := got.CarrierSelections.Get("https", "applied"); !ok || v != 7 {
		t.Fatalf("selections = %v, %v", v, ok)
	}
	if got.CarrierFailures != nil {
		t.Fatal("a family the node never reported must stay nil")
	}
	if _, ok := got.CarrierFailures.Total(); ok {
		t.Fatal("an absent family must not answer with a total")
	}
	if v, ok := got.BridgeRecovery.Total(); !ok || v != 0 {
		t.Fatalf("a declared but empty family is a real zero: %v, %v", v, ok)
	}
	back := WebTelemetryToProto(got)
	if back.GetCarrierFailures() != nil || back.GetBridgeRecovery() == nil {
		t.Fatalf("round trip changed presence: %+v", back)
	}
}

func TestCapabilitiesKeepUndeterminedDistinctFromUnsupported(t *testing.T) {
	caps := CapabilitiesFromProto(&agentv1.TelemtCapabilities{
		Supported:    map[string]bool{"Web": true, "WebDrain": false},
		Undetermined: []string{"CarrierLearning"},
		Build:        "x86_64-unknown-linux-gnu",
	})
	if v, ok := caps.Determined("Web"); !ok || !v {
		t.Fatalf("Web = %v, %v", v, ok)
	}
	if v, ok := caps.Determined("WebDrain"); !ok || v {
		t.Fatalf("WebDrain answered false and must stay determined: %v, %v", v, ok)
	}
	if _, ok := caps.Determined("CarrierLearning"); ok {
		t.Fatal("an undetermined capability must not read as determined")
	}
	raw, err := json.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]*bool
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if v, ok := decoded["CarrierLearning"]; !ok || v != nil {
		t.Fatalf("an undetermined capability must be stored as an explicit null: %s", raw)
	}
	if decoded["WebDrain"] == nil || *decoded["WebDrain"] {
		t.Fatalf("a determined false must stay false, not null: %s", raw)
	}
}
