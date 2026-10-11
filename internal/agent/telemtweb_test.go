package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tgwebproxy/internal/sitekit"

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
	web["http_connection_capacity_action"] = string(p.Overload.ConnectionCapacityAction)
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
	if patch.Web["http_connection_capacity_action"] != "wait" {
		t.Fatalf("overload action: %s", patches[0])
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

func TestWebAdvancedOptionsReachTelemt(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.version = "3.5.14"
	p := domain.DefaultWebPolicy()
	if err := json.Unmarshal([]byte(`{"carrier_method":"put","conveyor":true}`), &p); err != nil {
		t.Fatal(err)
	}
	res := h.Apply(context.Background(), webPolicyRequest(p))
	if !res.Ok {
		t.Fatal(res.Log)
	}
	web := ft.section("web")
	if web["carrier_method"] != "put" || web["conveyor"] != true {
		t.Fatalf("advanced options lost: %v", web)
	}
}

func TestWebBasePathApplyPreservesUnrelatedVhosts(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.version = "3.5.14"
	ft.mu.Lock()
	web := ft.config["web"].(map[string]any)
	vs := web["vhosts"].([]any)
	vs[0].(map[string]any)["future_field"] = "keep"
	web["vhosts"] = append(vs, map[string]any{"host": "other.test", "base_path": "untouched", "decoy": map[string]any{"mode": "http_upstream", "upstream": "http://127.0.0.1:99", "resolve": "startup"}})
	ft.mu.Unlock()
	p := domain.DefaultWebPolicy()
	if err := json.Unmarshal([]byte(`{"base_path":"telegram/web"}`), &p); err != nil {
		t.Fatal(err)
	}
	req := webPolicyRequest(p)
	req.ManagedWebHost = "n1.example.com"
	res := h.Apply(context.Background(), req)
	if !res.Ok {
		t.Fatal(res.Log)
	}
	if res.AppliedWebBasePath == nil || *res.AppliedWebBasePath != "telegram/web" {
		t.Fatalf("missing verified path: %+v", res)
	}
	raw := ft.section("web")["vhosts"].([]any)
	v := []map[string]any{raw[0].(map[string]any), raw[1].(map[string]any)}
	if v[0]["base_path"] != "telegram/web" || v[0]["future_field"] != "keep" || v[1]["base_path"] != "untouched" {
		t.Fatalf("vhosts corrupted: %v", v)
	}
}

func TestWebAdvancedOptionsRefuseOldEngine(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	p := domain.DefaultWebPolicy()
	if err := json.Unmarshal([]byte(`{"carrier_method":"put"}`), &p); err != nil {
		t.Fatal(err)
	}
	res := h.Apply(context.Background(), webPolicyRequest(p))
	if res.Ok {
		t.Fatal("old engine silently accepted advanced options")
	}
	if ft.section("web")["carrier_method"] != nil {
		t.Fatal("unsupported config was patched")
	}
}

func TestWebBasePathRollbackKeepsPreviousPrefix(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.version = "3.5.14"
	ft.mu.Lock()
	v := ft.config["web"].(map[string]any)["vhosts"].([]any)[0].(map[string]any)
	v["base_path"] = "old"
	v["future_field"] = "preserved"
	ft.mu.Unlock()
	ft.reloadStates = []string{"failed"}
	p := domain.DefaultWebPolicy()
	if err := json.Unmarshal([]byte(`{"base_path":"new"}`), &p); err != nil {
		t.Fatal(err)
	}
	req := webPolicyRequest(p)
	req.ManagedWebHost = "n1.example.com"
	res := h.Apply(context.Background(), req)
	if res.Ok || res.AppliedWebBasePath != nil {
		t.Fatalf("failed cutover advertised new path: %+v", res)
	}
	v = ft.section("web")["vhosts"].([]any)[0].(map[string]any)
	if v["base_path"] != "old" || v["future_field"] != "preserved" {
		t.Fatalf("rollback lost prefix or fields: %v", v)
	}
}

func TestWebDecoyResolvePreservesUpstreamAndUnknownSettings(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.version = "3.5.14"
	ft.mu.Lock()
	v := ft.config["web"].(map[string]any)["vhosts"].([]any)[0].(map[string]any)
	v["decoy"] = map[string]any{"mode": "http_upstream", "upstream": "http://example.internal:8080", "future": "keep"}
	ft.mu.Unlock()
	p := domain.DefaultWebPolicy()
	if err := json.Unmarshal([]byte(`{"decoy_resolve":"startup"}`), &p); err != nil {
		t.Fatal(err)
	}
	req := webPolicyRequest(p)
	req.ManagedWebHost = "n1.example.com"
	res := h.Apply(context.Background(), req)
	if !res.Ok {
		t.Fatal(res.Log)
	}
	d := ft.section("web")["vhosts"].([]any)[0].(map[string]any)["decoy"].(map[string]any)
	if d["resolve"] != "startup" || d["upstream"] != "http://example.internal:8080" || d["future"] != "keep" {
		t.Fatalf("decoy corrupted: %v", d)
	}
}

// Hostname origins must only be contacted by Telemt after its DNS validation and pinning.
type rejectDirectDNSOrigin struct {
	t      *testing.T
	target string
	calls  []string
}

func (r *rejectDirectDNSOrigin) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls = append(r.calls, req.URL.Hostname())
	if req.URL.Hostname() == "origin.internal" {
		return nil, fmt.Errorf("direct DNS origin bypassed Telemt validation")
	}
	return (decoyRoundTripper{target: r.target}).RoundTrip(req)
}

func TestWebHostnameOriginUsesEngineValidatedDecoyRoute(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.version = "3.5.14"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "private website") }))
	defer origin.Close()
	rt := &rejectDirectDNSOrigin{t: t, target: origin.URL}
	h.decoyHTTP = &http.Client{Transport: rt}
	p := domain.DefaultWebPolicy()
	if err := json.Unmarshal([]byte(`{"decoy_resolve":"startup"}`), &p); err != nil {
		t.Fatal(err)
	}
	req := webPolicyRequest(p)
	req.ManagedWebHost = "n1.example.com"
	req.Site = &agentv1.SiteBundle{Files: []*agentv1.SiteFile{{Path: sitekit.UpstreamMarkerPath, Content: []byte("http://origin.internal:8080")}}}
	res := h.Apply(context.Background(), req)
	if !res.Ok {
		t.Fatal(res.Log)
	}
	for _, host := range rt.calls {
		if host == "origin.internal" {
			t.Fatal("agent re-resolved pinned origin")
		}
	}
	d := ft.section("web")["vhosts"].([]any)[0].(map[string]any)["decoy"].(map[string]any)
	if d["resolve"] != "startup" || d["upstream"] != "http://origin.internal:8080" {
		t.Fatalf("DNS opt-in omitted: %v", d)
	}
}

func TestWebRejectedHostnameCandidateKeepsOldDecoy(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.version = "3.5.14"
	ft.failOn = "PATCH /v1/config"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "website") }))
	defer origin.Close()
	rt := &rejectDirectDNSOrigin{t: t, target: origin.URL}
	h.decoyHTTP = &http.Client{Transport: rt}
	p := domain.DefaultWebPolicy()
	if err := json.Unmarshal([]byte(`{"decoy_resolve":"startup"}`), &p); err != nil {
		t.Fatal(err)
	}
	req := webPolicyRequest(p)
	req.ManagedWebHost = "n1.example.com"
	req.Site = &agentv1.SiteBundle{Files: []*agentv1.SiteFile{{Path: sitekit.UpstreamMarkerPath, Content: []byte("http://origin.internal:8080")}}}
	res := h.Apply(context.Background(), req)
	if res.Ok || res.AppliedWebBasePath != nil {
		t.Fatalf("rejected origin activated: %+v", res)
	}
	if len(rt.calls) != 0 {
		t.Fatal("agent probed unvalidated candidate")
	}
	d := ft.section("web")["vhosts"].([]any)[0].(map[string]any)["decoy"].(map[string]any)
	if d["mode"] != "static_directory" || d["resolve"] != nil {
		t.Fatalf("failed candidate changed decoy: %v", d)
	}
}

func TestWebPrefixReadbackMismatchRollsBackWithoutAcknowledgment(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.version = "3.5.14"
	ft.ignoreBasePath = true
	ft.mu.Lock()
	v := ft.config["web"].(map[string]any)["vhosts"].([]any)[0].(map[string]any)
	v["base_path"] = "previous"
	ft.mu.Unlock()
	p := domain.DefaultWebPolicy()
	path := "next"
	p.BasePath = &path
	req := webPolicyRequest(p)
	req.ManagedWebHost = "n1.example.com"
	res := h.Apply(context.Background(), req)
	if res.Ok || res.AppliedWebBasePath != nil || !res.RolledBack {
		t.Fatalf("ignored prefix acknowledged: %+v", res)
	}
	v = ft.section("web")["vhosts"].([]any)[0].(map[string]any)
	if v["base_path"] != "previous" {
		t.Fatalf("previous prefix lost: %v", v)
	}
}

func TestWebRootResetAcknowledgesExplicitEmptyPath(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.version = "3.5.14"
	ft.mu.Lock()
	ft.config["web"].(map[string]any)["vhosts"].([]any)[0].(map[string]any)["base_path"] = "previous"
	ft.mu.Unlock()
	p := domain.DefaultWebPolicy()
	path := ""
	p.BasePath = &path
	res := h.Apply(context.Background(), webPolicyRequest(p))
	if !res.Ok || res.AppliedWebBasePath == nil || *res.AppliedWebBasePath != "" {
		t.Fatalf("explicit root ACK absent: %+v", res)
	}
}

func TestUnmanagedPrefixAcknowledgmentRequiresReloadBoundary(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.version = "3.5.14"
	settleWebPolicy(ft, domain.DefaultWebPolicy())
	ft.mu.Lock()
	ft.config["web"].(map[string]any)["vhosts"].([]any)[0].(map[string]any)["base_path"] = "saved/not-yet-active"
	ft.mu.Unlock()
	req := webPolicyRequest(domain.DefaultWebPolicy())
	req.ManagedWebHost = "n1.example.com"
	res := h.Apply(context.Background(), req)
	if !res.Ok {
		t.Fatal(res.Log)
	}
	_, _, _, reloads := ft.snapshot()
	if reloads == 0 {
		t.Fatal("persisted source prefix acknowledged without runtime reload")
	}
	if res.AppliedWebBasePath == nil || *res.AppliedWebBasePath != "saved/not-yet-active" {
		t.Fatalf("effective prefix ACK missing: %+v", res)
	}
}

func TestManagedHostApplyPreservesOtherVhostAndUsers(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.version = "3.5.14"
	ft.mu.Lock()
	web := ft.config["web"].(map[string]any)
	managed := web["vhosts"].([]any)[0]
	other := map[string]any{"host": "other.test", "base_path": "other/path", "public_addr": "203.0.113.9:443", "profiles": []any{map[string]any{"user": "outside", "secret_mode": "dd"}}, "decoy": map[string]any{"mode": "http_upstream", "upstream": "http://127.0.0.1:88", "resolve": "startup"}, "future": "keep"}
	web["vhosts"] = []any{other, managed}
	ft.users["outside"] = "0123456789abcdef0123456789abcdef"
	ft.attrs["outside"] = map[string]any{}
	ft.mu.Unlock()
	p := domain.DefaultWebPolicy()
	path := "managed/path"
	p.BasePath = &path
	req := webPolicyRequest(p)
	req.ManagedWebHost = "n1.example.com"
	req.PublicIp = "203.0.113.8"
	res := h.Apply(context.Background(), req)
	if !res.Ok {
		t.Fatal(res.Log)
	}
	vs := ft.section("web")["vhosts"].([]any)
	v := vs[0].(map[string]any)
	if v["base_path"] != "other/path" || v["public_addr"] != "203.0.113.9:443" || v["future"] != "keep" || v["profiles"].([]any)[0].(map[string]any)["user"] != "outside" {
		t.Fatalf("unrelated vhost changed: %v", v)
	}
	v = vs[1].(map[string]any)
	if v["base_path"] != "managed/path" || v["public_addr"] != "203.0.113.8:443" || v["profiles"].([]any)[0].(map[string]any)["user"] != "node" {
		t.Fatalf("managed vhost not applied: %v", v)
	}
	users, _, _, _ := ft.snapshot()
	if _, ok := users["outside"]; !ok {
		t.Fatal("unmanaged vhost user deleted")
	}
}

func TestMissingOrAmbiguousManagedHostRefusedBeforeMutation(t *testing.T) {
	for _, host := range []string{"missing.test", "n1.example.com"} {
		t.Run(host, func(t *testing.T) {
			h, _, ft := telemtHandler(t, &fakeExec{})
			if host == "n1.example.com" {
				ft.mu.Lock()
				web := ft.config["web"].(map[string]any)
				web["vhosts"] = append(web["vhosts"].([]any), map[string]any{"host": "n1.example.com"})
				ft.mu.Unlock()
			}
			req := webPolicyRequest(domain.DefaultWebPolicy())
			req.ManagedWebHost = host
			res := h.Apply(context.Background(), req)
			if res.Ok {
				t.Fatalf("missing/ambiguous managed host accepted: %+v", res)
			}
			_, writes, _, _ := ft.snapshot()
			if len(writes) != 0 {
				t.Fatalf("invalid target mutated node: %v", writes)
			}
		})
	}
}

func TestWebPrefixAcknowledgmentRejectsSourceRevisionChangedAfterReload(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.version = "3.5.14"
	settleWebPolicy(ft, domain.DefaultWebPolicy())
	ft.mu.Lock()
	ft.config["web"].(map[string]any)["vhosts"].([]any)[0].(map[string]any)["base_path"] = "loaded"
	ft.editAfterReload = "edited-after-reload"
	ft.mu.Unlock()
	req := webPolicyRequest(domain.DefaultWebPolicy())
	req.ManagedWebHost = "n1.example.com"
	res := h.Apply(context.Background(), req)
	if res.Ok || res.AppliedWebBasePath != nil {
		t.Fatalf("disk revision confused with runtime: %+v", res)
	}
}
