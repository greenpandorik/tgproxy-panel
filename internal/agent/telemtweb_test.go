package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodedriver"
	agentv1 "tgwebproxy/proto/agent/v1"
)

// settleWebPolicy writes a policy into the fake's [web] section the way telemt would hold it.
func settleWebPolicy(f *fakeTelemt, p domain.WebPolicy) {
	f.mu.Lock()
	defer f.mu.Unlock()
	web, _ := f.config["web"].(map[string]any)
	carriers := make([]any, 0, len(p.Carriers))
	for _, c := range p.Carriers {
		carriers = append(carriers, string(c))
	}
	deadlines := make([]any, 0, len(p.Timeouts.NegotiationDeadlinesSecs))
	for _, d := range p.Timeouts.NegotiationDeadlinesSecs {
		deadlines = append(deadlines, float64(d))
	}
	web["carrier"] = string(p.Carrier)
	web["carriers"] = carriers
	web["carrier_learning"] = p.CarrierLearning
	web["carrier_negotiation_aggressiveness"] = string(p.Aggressiveness)
	web["timeouts"] = map[string]any{
		"carrier_negotiation_deadlines_secs": deadlines,
		"carrier_health_secs":                float64(p.Timeouts.CarrierHealthSecs),
		"carrier_learning_secs":              float64(p.Timeouts.CarrierLearningSecs),
		"bridge_request_secs":                float64(p.Timeouts.BridgeRequestSecs),
		"bridge_retry_secs":                  float64(p.Timeouts.BridgeRetrySecs),
		"carrier_probe_coalesce_ms":          float64(p.Timeouts.ProbeCoalesceMs),
	}
}

func webPolicyRequest(p domain.WebPolicy) *agentv1.ApplyRequest {
	return &agentv1.ApplyRequest{
		ApplyProfiles: true,
		Profiles:      profiles("node", secretNode),
		WebPolicy:     nodedriver.WebPolicyToProto(&p),
	}
}

func TestTelemtApplyWithoutAWebPolicyLeavesTheWebConfigAlone(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{ApplyProfiles: true, Profiles: profiles("node", secretNode)})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	web := ft.section("web")
	if _, set := web["carrier_learning"]; set {
		t.Fatalf("an agent given no policy must not invent one: %v", web)
	}
}

func TestTelemtApplyWritesTheWebPolicy(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	res := h.Apply(context.Background(), webPolicyRequest(domain.DefaultWebPolicy()))
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	if res.RestartedRelay {
		t.Fatalf("the web policy is hot-reloadable and must not restart telemt: %s", res.Log)
	}
	_, _, patches, _ := ft.snapshot()
	if len(patches) != 1 {
		t.Fatalf("expected one config patch, got %v", patches)
	}
	var patch struct {
		Web map[string]any `json:"web"`
	}
	if err := json.Unmarshal([]byte(patches[0]), &patch); err != nil {
		t.Fatal(err)
	}
	if patch.Web["carrier"] != nil {
		t.Fatalf("carrier already matched and must not be patched: %s", patches[0])
	}
	carriers, _ := patch.Web["carriers"].([]any)
	if len(carriers) != 3 || carriers[0] != "websocket-lanes" || carriers[2] != "https-lanes" {
		t.Fatalf("carriers: %s", patches[0])
	}
	if patch.Web["carrier_learning"] != true || patch.Web["carrier_negotiation_aggressiveness"] != "conservative" {
		t.Fatalf("negotiation keys: %s", patches[0])
	}
	timeouts, _ := patch.Web["timeouts"].(map[string]any)
	if len(timeouts) != 6 || timeouts["carrier_health_secs"] != 30.0 || timeouts["bridge_retry_secs"] != 90.0 {
		t.Fatalf("timeouts: %s", patches[0])
	}
	deadlines, _ := timeouts["carrier_negotiation_deadlines_secs"].([]any)
	if len(deadlines) != 4 || deadlines[0] != 3.0 || deadlines[3] != 12.0 {
		t.Fatalf("deadlines: %s", patches[0])
	}
	if _, set := patch.Web["vhosts"]; set {
		t.Fatalf("the policy patch must not carry the vhosts with it: %s", patches[0])
	}
}

func TestTelemtWebPolicyIsIdempotent(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	settleWebPolicy(ft, domain.DefaultWebPolicy())
	req := webPolicyRequest(domain.DefaultWebPolicy())
	if res := h.Apply(context.Background(), req); !res.Ok {
		t.Fatalf("first apply: %s", res.Log)
	}
	ft.resetWrites()
	res := h.Apply(context.Background(), req)
	if !res.Ok {
		t.Fatalf("second apply: %s", res.Log)
	}
	_, writes, patches, reloads := ft.snapshot()
	if len(writes) != 0 || len(patches) != 0 || reloads != 0 {
		t.Fatalf("a settled policy must cost no writes: %v %v %d\n%s", writes, patches, reloads, res.Log)
	}
}

// The fallback carrier telemt appends to the negotiation order it reports is not a difference.
func TestTelemtWebPolicyToleratesTheAppendedFallback(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	settled := domain.DefaultWebPolicy()
	settled.Carriers = append(settled.Carriers, settled.Carrier)
	settleWebPolicy(ft, settled)
	if res := h.Apply(context.Background(), webPolicyRequest(domain.DefaultWebPolicy())); !res.Ok {
		t.Fatalf("apply: %s", res.Log)
	}
	_, _, patches, _ := ft.snapshot()
	if len(patches) != 0 {
		t.Fatalf("expected no patch, got %v", patches)
	}
}

func TestTelemtWebPolicyPatchesOnlyWhatMoved(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	settleWebPolicy(ft, domain.DefaultWebPolicy())
	want := domain.DefaultWebPolicy()
	want.Aggressiveness = domain.AggressivenessAggressive
	want.Timeouts.CarrierLearningSecs = 1200
	if res := h.Apply(context.Background(), webPolicyRequest(want)); !res.Ok {
		t.Fatalf("apply: %s", res.Log)
	}
	_, _, patches, _ := ft.snapshot()
	if len(patches) != 1 {
		t.Fatalf("expected one patch, got %v", patches)
	}
	var patch struct {
		Web map[string]any `json:"web"`
	}
	if err := json.Unmarshal([]byte(patches[0]), &patch); err != nil {
		t.Fatal(err)
	}
	if len(patch.Web) != 2 || patch.Web["carrier_negotiation_aggressiveness"] != "aggressive" {
		t.Fatalf("patch carries more than the two changed keys: %s", patches[0])
	}
	timeouts, _ := patch.Web["timeouts"].(map[string]any)
	if len(timeouts) != 1 || timeouts["carrier_learning_secs"] != 1200.0 {
		t.Fatalf("timeouts: %s", patches[0])
	}
}

func TestTelemtWebPolicyReportsADeferredPatch(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	settleWebPolicy(ft, domain.DefaultWebPolicy())
	ft.deferFields = []string{"web.carrier_learning"}
	want := domain.DefaultWebPolicy()
	want.CarrierLearning = false
	res := h.Apply(context.Background(), webPolicyRequest(want))
	if !res.Ok {
		t.Fatalf("apply: %s", res.Log)
	}
	if len(res.DeferredFields) != 1 || res.DeferredFields[0] != "web.carrier_learning" {
		t.Fatalf("deferred fields must reach the panel: %+v", res.DeferredFields)
	}
	if !res.RestartRequired {
		t.Fatalf("a deferred patch must say a restart is required: %+v", res)
	}
	if res.RestartedRelay {
		t.Fatalf("the agent must report the deferral, not restart behind the operator: %s", res.Log)
	}
	if !strings.Contains(res.Log, "did not activate web.carrier_learning") {
		t.Fatalf("apply log: %s", res.Log)
	}
}

func TestTelemtWebPolicyOutOfBoundsIsRefusedBeforeAnyCall(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	bad := domain.DefaultWebPolicy()
	bad.Timeouts.BridgeRequestSecs = 600
	res := h.Apply(context.Background(), webPolicyRequest(bad))
	if res.Ok {
		t.Fatalf("a policy telemt would refuse must not be applied: %s", res.Log)
	}
	if !strings.Contains(res.Log, "bridge_request_secs") {
		t.Fatalf("apply log: %s", res.Log)
	}
	_, _, patches, _ := ft.snapshot()
	if len(patches) != 0 {
		t.Fatalf("nothing may be patched: %v", patches)
	}
}

func TestTelemtWebPolicyRollsBackWhenALaterStepFails(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	settleWebPolicy(ft, domain.DefaultWebPolicy())
	if res := h.Apply(context.Background(), webPolicyRequest(domain.DefaultWebPolicy())); !res.Ok {
		t.Fatalf("setup apply: %s", res.Log)
	}
	ft.mu.Lock()
	ft.ready = false
	ft.mu.Unlock()
	want := domain.DefaultWebPolicy()
	want.Aggressiveness = domain.AggressivenessBalanced
	res := h.Apply(context.Background(), webPolicyRequest(want))
	if res.Ok {
		t.Fatalf("apply should have failed: %s", res.Log)
	}
	web := ft.section("web")
	if web["carrier_negotiation_aggressiveness"] != "conservative" {
		t.Fatalf("the previous policy must be restored: %v", web)
	}
}
