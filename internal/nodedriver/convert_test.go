package nodedriver

import (
	"reflect"
	"testing"
	"time"

	"tgwebproxy/internal/domain"
)

func TestProfileRoundTrip(t *testing.T) {
	in := Profile{
		Name: "k1", Secret: "00", Backend: "127.0.0.1:2398", CarrierMode: "https-lanes",
		Limits: &domain.ProfileLimits{MaxSessions: 5, MaxStreamsPerSession: 2},
	}
	out := ProfileFromProto(ProfileToProto(in))
	if out.Name != in.Name || out.Secret != in.Secret || out.CarrierMode != in.CarrierMode || out.Limits == nil || out.Limits.MaxSessions != 5 {
		t.Fatalf("round trip lost data: %+v", out)
	}
	noLimits := ProfileFromProto(ProfileToProto(Profile{Name: "a"}))
	if noLimits.Limits != nil {
		t.Fatal("nil limits must stay nil")
	}
}

func TestSiteRoundTrip(t *testing.T) {
	in := SiteBundle{Files: map[string][]byte{"index.html": []byte("<p>x</p>"), "s.css": []byte("p{}")}}
	out := SiteFromProto(SiteToProto(in))
	if len(out.Files) != 2 || string(out.Files["index.html"]) != "<p>x</p>" {
		t.Fatalf("bad %+v", out)
	}
}

func TestProfileTelemtFieldsRoundTrip(t *testing.T) {
	exp := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	in := Profile{
		Name: "k1", Secret: "aa", Enabled: true, ExpiresAt: &exp,
		Telemt: &domain.TelemtLimits{
			DataQuotaBytes: 1 << 30, RateLimitUpBps: 1_000_000, RateLimitDownBps: 2_000_000,
			MaxUniqueIPs: 3, MaxTCPConns: 64,
		},
	}
	out := ProfileFromProto(ProfileToProto(in))
	if out.Telemt == nil || *out.Telemt != *in.Telemt {
		t.Fatalf("telemt limits lost: %+v", out.Telemt)
	}
	if out.ExpiresAt == nil || !out.ExpiresAt.Equal(exp) {
		t.Fatalf("expiry lost: %v", out.ExpiresAt)
	}
	if !out.Enabled {
		t.Fatal("enabled lost")
	}
}

func TestProfileWithoutTelemtLimitsStaysNil(t *testing.T) {
	out := ProfileFromProto(ProfileToProto(Profile{Name: "a", Enabled: true}))
	if out.Telemt != nil {
		t.Fatalf("zero limits must stay nil, got %+v", out.Telemt)
	}
	if out.ExpiresAt != nil {
		t.Fatalf("nil expiry must stay nil, got %v", out.ExpiresAt)
	}
	if !out.Enabled {
		t.Fatal("enabled lost")
	}
}

// A disabled profile must survive the round trip as disabled.
func TestProfileDisabledRoundTrip(t *testing.T) {
	out := ProfileFromProto(ProfileToProto(Profile{Name: "a", Enabled: false}))
	if out.Enabled {
		t.Fatal("disabled profile came back enabled")
	}
}

func TestHealthDcFieldsRoundTrip(t *testing.T) {
	in := HealthReport{
		RelayActive: true, CPUPercent: 1.5,
		DCs: []DcLatency{
			{DC: 1, LatencyMs: 197.9, Known: true, IPPreference: "prefer_v4"},
			{DC: 4, Known: false, IPPreference: "prefer_v6"},
		},
		UpstreamHealthy: true, UpstreamFails: 2, EffectiveLatencyMs: 41.25,
		ConnectSuccessTotal: 58, ConnectFailTotal: 1, UpstreamLastCheckAgeSecs: 29, DcDataAvailable: true,
	}
	out := HealthFromProto(HealthToProto(in))
	if len(out.DCs) != 2 || out.DCs[0] != in.DCs[0] || out.DCs[1] != in.DCs[1] {
		t.Fatalf("dcs lost: %+v", out.DCs)
	}
	if !out.UpstreamHealthy || out.UpstreamFails != 2 || out.EffectiveLatencyMs != 41.25 ||
		out.ConnectSuccessTotal != 58 || out.ConnectFailTotal != 1 || out.UpstreamLastCheckAgeSecs != 29 || !out.DcDataAvailable {
		t.Fatalf("upstream fields lost: %+v", out)
	}
	if empty := HealthFromProto(HealthToProto(HealthReport{})); empty.DcDataAvailable || len(empty.DCs) != 0 {
		t.Fatalf("a report without DC data must stay unavailable: %+v", empty)
	}
}

func TestWebPolicyProtoRoundTrip(t *testing.T) {
	if got := WebPolicyToProto(nil); got != nil {
		t.Fatalf("no policy must stay no policy: %+v", got)
	}
	if got := WebPolicyFromProto(nil); got != nil {
		t.Fatalf("no policy must stay no policy: %+v", got)
	}
	want := domain.DefaultWebPolicy()
	want.Carriers = []domain.Carrier{domain.CarrierWebSocket, domain.CarrierHTTPSLanes}
	want.CarrierLearning = false
	want.Timeouts.ProbeCoalesceMs = 7
	want.Overload.ConnectionCapacityAction = domain.WebCapacityRespond
	got := WebPolicyFromProto(WebPolicyToProto(&want))
	if got == nil {
		t.Fatal("nil round trip")
	}
	if got.Carrier != want.Carrier || got.CarrierLearning || got.Aggressiveness != want.Aggressiveness {
		t.Fatalf("policy %+v", got)
	}
	if !reflect.DeepEqual(got.Carriers, want.Carriers) || !reflect.DeepEqual(got.Timeouts, want.Timeouts) {
		t.Fatalf("carriers %+v timeouts %+v", got.Carriers, got.Timeouts)
	}
	if got.Overload.ConnectionCapacityAction != domain.WebCapacityRespond {
		t.Fatalf("overload policy lost: %+v", got.Overload)
	}
}
