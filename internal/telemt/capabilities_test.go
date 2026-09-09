package telemt

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func telemtConfig() map[string]any {
	return map[string]any{
		"general":    map[string]any{"use_middle_proxy": false},
		"censorship": map[string]any{"tls_domain": "example.com", "tls_emulation": true},
		"web": map[string]any{
			"enabled": true,
			"vhosts": []any{map[string]any{
				"host":  "n.test",
				"decoy": map[string]any{"mode": "static_directory", "directory": "/var/lib/telemt/public"},
			}},
		},
	}
}

func TestCapabilitiesFromPinnedVersion(t *testing.T) {
	caps, unknown := ComputeCapabilities(CapabilityProbe{Version: "3.5.7", Config: telemtConfig()})
	want := TelemtCapabilities{
		Web: true, CarrierNegotiation: true, CarrierLearning: true, CarrierLearningReset: true,
		WebPause: true, WebDrain: true, WebResume: true, WebRuntime: true,
		HttpUpstreamDecoy: true, TLSEmulation: true, MiddleProxy: true,
	}
	if caps != want {
		t.Fatalf("caps: %+v", caps)
	}
	if unknown.Any() {
		t.Fatalf("nothing should be unknown for a pinned build: %v", unknown.Names())
	}
}

func TestCapabilitiesFromOlderVersion(t *testing.T) {
	caps, unknown := ComputeCapabilities(CapabilityProbe{Version: "telemt 3.5.5", Config: telemtConfig()})
	if !caps.Web || !caps.CarrierNegotiation || !caps.TLSEmulation || !caps.MiddleProxy {
		t.Fatalf("web-era capabilities lost: %+v", caps)
	}
	if caps.WebRuntime || caps.WebPause || caps.WebDrain || caps.WebResume {
		t.Fatalf("3.5.5 has no web lifecycle: %+v", caps)
	}
	if caps.CarrierLearning || caps.CarrierLearningReset {
		t.Fatalf("3.5.5 has no carrier learning: %+v", caps)
	}
	if unknown.Any() {
		t.Fatalf("a parsed version answers every capability: %v", unknown.Names())
	}
}

func TestUnknownVersionLeavesCapabilitiesUnknownNotFalse(t *testing.T) {
	caps, unknown := ComputeCapabilities(CapabilityProbe{Version: "", StatusErr: errors.New("dial tcp: connection refused"), ConfigErr: errors.New("dial tcp: connection refused")})
	if caps != (TelemtCapabilities{}) {
		t.Fatalf("nothing may be claimed without evidence: %+v", caps)
	}
	if len(unknown.Names()) != 11 {
		t.Fatalf("every capability must be unknown: %v", unknown.Names())
	}
	for _, name := range []string{CapWeb, CapWebDrain, CapCarrierLearningReset, CapMiddleProxy} {
		if !unknown.Has(name) {
			t.Fatalf("%s reported as unsupported when it was never determined", name)
		}
	}
}

func TestProbeProvesCapabilitiesAnUnreadableVersionCannot(t *testing.T) {
	learning := &WebLearning{Enabled: true}
	st := WebStatus{
		RuntimeInstance:    "aa",
		OperatorLifecycle:  &WebLifecycle{State: WebLifecycleRunning},
		Runtime:            WebRuntime{Learning: learning},
		CarrierNegotiation: []byte(`{"selection":{}}`),
	}
	caps, unknown := ComputeCapabilities(CapabilityProbe{Version: "dev", Status: &st, Config: telemtConfig()})
	if !caps.Web || !caps.WebRuntime || !caps.WebPause || !caps.WebDrain || !caps.WebResume {
		t.Fatalf("lifecycle proven by the status payload: %+v", caps)
	}
	if !caps.CarrierNegotiation || !caps.CarrierLearning {
		t.Fatalf("sections present in the payload prove the capability: %+v", caps)
	}
	if !caps.TLSEmulation || !caps.MiddleProxy {
		t.Fatalf("config keys prove the capability: %+v", caps)
	}
	if caps.CarrierLearningReset || !unknown.Has(CapCarrierLearningReset) {
		t.Fatal("reset can only be probed by mutating, so it must stay unknown here")
	}
	if caps.HttpUpstreamDecoy || !unknown.Has(CapHTTPUpstreamDecoy) {
		t.Fatal("a static decoy says nothing about http_upstream support")
	}
}

func TestHTTPUpstreamDecoyProvenByConfig(t *testing.T) {
	cfg := telemtConfig()
	web := cfg["web"].(map[string]any)
	vhost := web["vhosts"].([]any)[0].(map[string]any)
	vhost["decoy"] = map[string]any{"mode": "http_upstream", "http_upstream": "http://127.0.0.1:3000"}
	caps, unknown := ComputeCapabilities(CapabilityProbe{Version: "dev", Config: cfg})
	if !caps.HttpUpstreamDecoy || unknown.Has(CapHTTPUpstreamDecoy) {
		t.Fatalf("configured http_upstream decoy: %+v", caps)
	}
}

func TestMissingWebRuntimeEndpointIsASupportedNo(t *testing.T) {
	notFound := &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "Not Found"}
	caps, unknown := ComputeCapabilities(CapabilityProbe{Version: "dev", StatusErr: notFound, Config: telemtConfig()})
	for _, name := range []string{CapWebRuntime, CapWebPause, CapWebDrain, CapWebResume, CapCarrierLearning, CapCarrierLearningReset} {
		if unknown.Has(name) {
			t.Fatalf("%s: a 404 is an answer, not a failure to ask", name)
		}
	}
	if caps.WebRuntime || caps.WebPause || caps.CarrierLearning || caps.CarrierLearningReset {
		t.Fatalf("caps: %+v", caps)
	}
	if !caps.Web {
		t.Fatal("the config still proves web mode exists")
	}
}

func TestWebRuntimeErrorOtherThanNotFoundProvesTheEndpointExists(t *testing.T) {
	busy := &APIError{Status: http.StatusConflict, Code: ErrCodeLifecycleInProgress, Message: "busy"}
	caps, unknown := ComputeCapabilities(CapabilityProbe{Version: "dev", StatusErr: busy})
	if !caps.WebRuntime || unknown.Has(CapWebRuntime) {
		t.Fatalf("telemt answered from that route: %+v", caps)
	}
	if !unknown.Has(CapWebPause) || !unknown.Has(CapCarrierLearning) {
		t.Fatal("an error body proves nothing about lifecycle or learning")
	}
}

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want [3]int
		ok   bool
	}{
		{"3.5.7", [3]int{3, 5, 7}, true},
		{"telemt 3.5.7", [3]int{3, 5, 7}, true},
		{"v3.6.0-rc1", [3]int{3, 6, 0}, true},
		{"3.5", [3]int{}, false},
		{"", [3]int{}, false},
		{"dev", [3]int{}, false},
	} {
		got, ok := ParseVersion(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("%q: got %v %v want %v %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestClientCapabilitiesProbesReadOnlyEndpoints(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/system/info":
			ok(w, `{"version":"3.5.7","target_arch":"x86_64"}`)
		case "/v1/runtime/web/status":
			ok(w, webStatusBody)
		case "/v1/config":
			ok(w, `{"web":{"enabled":true},"censorship":{"tls_emulation":true},"general":{"use_middle_proxy":false}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	caps, unknown, err := c.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !caps.Web || !caps.WebDrain || !caps.CarrierLearning || !caps.CarrierLearningReset {
		t.Fatalf("caps: %+v", caps)
	}
	if unknown.Any() {
		t.Fatalf("unknown: %v", unknown.Names())
	}
	for _, got := range *calls {
		if got.method != http.MethodGet {
			t.Fatalf("capability probing must never write: %+v", got)
		}
	}
}

func TestClientCapabilitiesFailsWhenTheNodeCannotBeAsked(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"unauthorized","message":"bad token"}}`))
	})
	if _, _, err := c.Capabilities(context.Background()); err == nil {
		t.Fatal("an unreachable control API must be an error, not an empty capability set")
	}
}
