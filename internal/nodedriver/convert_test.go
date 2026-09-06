package nodedriver

import (
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

// A profile with no telemt limits must not materialise an all-zero limits struct on the far
// side: the agent uses non-nil to mean "the panel has an opinion about these fields".
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

// A disabled profile must survive the round trip as disabled - proto3 has no presence for a
// bare bool, so the panel writes it literally and the agent reads it literally.
func TestProfileDisabledRoundTrip(t *testing.T) {
	out := ProfileFromProto(ProfileToProto(Profile{Name: "a", Enabled: false}))
	if out.Enabled {
		t.Fatal("disabled profile came back enabled")
	}
}
