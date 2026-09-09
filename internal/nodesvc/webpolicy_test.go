package nodesvc_test

import (
	"encoding/json"
	"testing"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodesvc"
)

func TestWebPolicyOfEmptyColumnIsTheDefault(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("{}"), []byte("not json")} {
		got := nodesvc.WebPolicyOf(raw)
		if err := got.Validate(); err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		want := domain.DefaultWebPolicy()
		if got.Carrier != want.Carrier || len(got.Carriers) != len(want.Carriers) ||
			got.CarrierLearning != want.CarrierLearning || got.Aggressiveness != want.Aggressiveness {
			t.Fatalf("%q: %+v", raw, got)
		}
		if got.Timeouts.CarrierLearningSecs != want.Timeouts.CarrierLearningSecs {
			t.Fatalf("%q: timeouts %+v", raw, got.Timeouts)
		}
	}
}

func TestWebPolicyOfMergesOverridesOverTheDefault(t *testing.T) {
	got := nodesvc.WebPolicyOf([]byte(`{"preset":"custom","timeouts":{"carrier_health_secs":45}}`))
	if got.Preset != domain.PresetCustom {
		t.Fatalf("preset %q", got.Preset)
	}
	if got.Timeouts.CarrierHealthSecs != 45 {
		t.Fatalf("override lost: %+v", got.Timeouts)
	}
	if got.Timeouts.BridgeRetrySecs != domain.DefaultWebPolicy().Timeouts.BridgeRetrySecs {
		t.Fatalf("untouched timeout must stay at the default: %+v", got.Timeouts)
	}
	if len(got.Carriers) != len(domain.DefaultWebPolicy().Carriers) {
		t.Fatalf("carriers: %+v", got.Carriers)
	}
}

func TestWebPolicyOverridesStoreOnlyWhatMoved(t *testing.T) {
	raw, err := nodesvc.WebPolicyOverrides(domain.DefaultWebPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{}" {
		t.Fatalf("the default must store nothing: %s", raw)
	}

	p := domain.DefaultWebPolicy()
	p.Preset = domain.PresetHTTPSOnly
	p.Carriers = []domain.Carrier{domain.CarrierHTTPS}
	p.Timeouts.BridgeRetrySecs = 120
	raw, err = nodesvc.WebPolicyOverrides(p)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 || stored["preset"] != "https_only" {
		t.Fatalf("stored: %s", raw)
	}
	timeouts, _ := stored["timeouts"].(map[string]any)
	if len(timeouts) != 1 || timeouts["bridge_retry_secs"] != 120.0 {
		t.Fatalf("stored timeouts: %s", raw)
	}
	if back := nodesvc.WebPolicyOf(raw); back.Timeouts.BridgeRetrySecs != 120 ||
		len(back.Carriers) != 1 || back.Preset != domain.PresetHTTPSOnly {
		t.Fatalf("round trip: %+v", back)
	}
}
