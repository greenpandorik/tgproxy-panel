package agent

import (
	"context"
	"log/slog"
	"testing"

	"tgwebproxy/internal/telemt"
	agentv1 "tgwebproxy/proto/agent/v1"
)

const webMetricsText = `# HELP telemt_web_carrier_selections_total x
# TYPE telemt_web_carrier_selections_total counter
telemt_web_carrier_selections_total{carrier="https",disposition="applied"} 12
telemt_web_carrier_selections_total{carrier="websocket-lanes",disposition="applied"} 30
telemt_web_carrier_selections_total{carrier="websocket-lanes",disposition="rejected"} 2
# HELP telemt_web_rejections_total x
# TYPE telemt_web_rejections_total counter
telemt_web_rejections_total{reason="capacity"} 4
# HELP telemt_web_carrier_learning_entries x
# TYPE telemt_web_carrier_learning_entries gauge
telemt_web_carrier_learning_entries{kind="carrier"} 9
`

func TestHealthWithoutTelemtReportsNoWebTelemetry(t *testing.T) {
	ex := &fakeExec{}
	_, cfg, _ := telemtHandler(t, ex)
	cfg.TelemtAPI = "http://127.0.0.1:1"
	cfg.TelemtMetricsURL = "http://127.0.0.1:1/metrics"
	dead := withConfig(t, cfg, ex)

	rep := dead.Health(context.Background())
	if rep.GetWeb() != nil {
		t.Fatalf("an unreachable telemt must report no WEB telemetry, got %+v", rep.GetWeb())
	}
	if rep.GetCapabilities() != nil {
		t.Fatalf("an unreachable telemt must report no capabilities, got %+v", rep.GetCapabilities())
	}
}

func TestWebTelemetryIsAbsentOnATproxyNode(t *testing.T) {
	h := NewHandler(Config{SiteDir: t.TempDir()}, &fakeExec{}, slog.New(slog.DiscardHandler))
	if got := h.webTelemetry(context.Background()); got != nil {
		t.Fatalf("a node with no telemt must report nothing, got %+v", got)
	}
	if got := h.telemtCapabilities(context.Background()); got != nil {
		t.Fatalf("a node with no telemt must report no capabilities, got %+v", got)
	}
}

func TestWebTelemetryCarriesOnlyTheReportedFamilies(t *testing.T) {
	ex := &fakeExec{}
	h, _, ft := telemtHandler(t, ex)
	ft.mu.Lock()
	ft.metricsText = webMetricsText
	ft.mu.Unlock()

	got := h.webTelemetry(context.Background())
	if got == nil {
		t.Fatal("expected telemetry")
	}
	if v, ok := familyValue(got.GetCarrierSelections(), "https", "applied"); !ok || v != 12 {
		t.Fatalf("https selections = %v, %v", v, ok)
	}
	if v, ok := familyValue(got.GetCarrierSelections(), "websocket-lanes", "rejected"); !ok || v != 2 {
		t.Fatalf("rejected selections = %v, %v", v, ok)
	}
	if got.GetRejections() == nil || len(got.GetRejections().GetSamples()) != 1 {
		t.Fatalf("rejections = %+v", got.GetRejections())
	}
	for name, f := range map[string]*agentv1.WebCounterFamily{
		"carrier_failures":  got.GetCarrierFailures(),
		"session_closures":  got.GetSessionClosures(),
		"bridge_recovery":   got.GetBridgeRecovery(),
		"learning_outcomes": got.GetLearningOutcomes(),
	} {
		if f != nil {
			t.Fatalf("%s was not in the exposition and must stay absent, got %+v", name, f)
		}
	}
}

func TestWebTelemetryCarriesTheRuntimeState(t *testing.T) {
	ex := &fakeExec{}
	h, _, ft := telemtHandler(t, ex)
	ft.mu.Lock()
	ft.metricsText = webMetricsText
	ft.webStatus = map[string]any{
		"runtime_instance":    "inst-1",
		"carrier_negotiation": map[string]any{"selections": map[string]any{}},
		"operator_lifecycle": map[string]any{
			"state": "paused", "epoch": 3, "admission_open": false,
			"effective_new_work_admission": false,
		},
		"runtime": map[string]any{"learning": map[string]any{"enabled": true, "entries": 9, "capacity": 1024}},
	}
	ft.mu.Unlock()

	rt := h.webTelemetry(context.Background()).GetRuntime()
	if rt == nil || rt.GetRuntimeInstance() != "inst-1" || !rt.GetCarrierNegotiation() {
		t.Fatalf("runtime = %+v", rt)
	}
	if rt.GetLearning() == nil || rt.GetLearning().GetEntries() != 9 {
		t.Fatalf("learning = %+v", rt.GetLearning())
	}
	if rt.GetLifecycle() == nil || rt.GetLifecycle().GetState() != "paused" || rt.GetLifecycle().GetAdmissionOpen() {
		t.Fatalf("lifecycle = %+v", rt.GetLifecycle())
	}
}

func TestWebTelemetrySurvivesAMissingWebRuntime(t *testing.T) {
	ex := &fakeExec{}
	h, _, ft := telemtHandler(t, ex)
	ft.mu.Lock()
	ft.metricsText = webMetricsText
	ft.mu.Unlock()

	got := h.webTelemetry(context.Background())
	if got == nil || got.GetRuntime() != nil {
		t.Fatalf("a node whose WEB status 404s reports metrics and no runtime, got %+v", got)
	}
}

func TestCapabilitiesKeepUndeterminedOutOfTheSupportedSet(t *testing.T) {
	caps := telemt.TelemtCapabilities{Web: true}
	unknown := telemt.UnknownCapabilities{telemt.CapCarrierLearning: struct{}{}}
	out := capabilitiesToProto(caps, unknown)
	if v, ok := out.GetSupported()[telemt.CapWeb]; !ok || !v {
		t.Fatalf("Web must be reported as supported: %+v", out.GetSupported())
	}
	if _, ok := out.GetSupported()[telemt.CapCarrierLearning]; ok {
		t.Fatal("an undetermined capability must not appear as false in the supported set")
	}
	if len(out.GetUndetermined()) != 1 || out.GetUndetermined()[0] != telemt.CapCarrierLearning {
		t.Fatalf("undetermined = %v", out.GetUndetermined())
	}
}

func TestCapabilityProbeIsReusedBetweenHeartbeats(t *testing.T) {
	ex := &fakeExec{}
	h, _, ft := telemtHandler(t, ex)
	first := h.telemtCapabilities(context.Background())
	if first == nil {
		t.Fatal("expected a capability set")
	}
	ft.mu.Lock()
	ft.failOn = "GET /v1/system/info"
	ft.mu.Unlock()
	if second := h.telemtCapabilities(context.Background()); second != first {
		t.Fatal("a second heartbeat must reuse the cached probe rather than re-asking telemt")
	}
}

func familyValue(f *agentv1.WebCounterFamily, carrier, label string) (float64, bool) {
	for _, s := range f.GetSamples() {
		if s.GetCarrier() == carrier && s.GetLabel() == label {
			return s.GetValue(), true
		}
	}
	return 0, false
}
