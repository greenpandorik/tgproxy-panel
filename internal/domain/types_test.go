package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCarrierModeValid(t *testing.T) {
	for _, m := range []CarrierMode{"https", "https-lanes", "websocket", "websocket-lanes"} {
		if !m.Valid() {
			t.Errorf("%s should be valid", m)
		}
	}
	if CarrierMode("tcp").Valid() || CarrierMode("").Valid() {
		t.Error("invalid modes accepted")
	}
}

func TestValidateSecretHex(t *testing.T) {
	if err := ValidateSecretHex("0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "dd0123456789abcdef0123456789abcdef", "0123456789ABCDEF0123456789ABCDEF", "zz"} {
		if ValidateSecretHex(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValidateHostname(t *testing.T) {
	if err := ValidateHostname("proxy.example.com"); err != nil {
		t.Fatal(err)
	}
	for _, good := range []string{"a.b", "x-1.y-2.example.com", strings.Repeat("a", 63) + ".com"} {
		if err := ValidateHostname(good); err != nil {
			t.Errorf("%q rejected: %v", good, err)
		}
	}
	bad := []string{
		"", "Proxy.Example.com", "localhost", "https://a.b", "a.b/", "-a.b",
		"a..b",         // empty label
		"a.-b.c",       // label starts with a hyphen
		"a.b-.c",       // label ends with a hyphen
		".a.b", "a.b.", // empty first/last label
		strings.Repeat("a", 64) + ".com",    // 64-char label
		strings.Repeat("a.", 127) + "bbbbb", // 254 chars total
	}
	for _, b := range bad {
		if ValidateHostname(b) == nil {
			t.Errorf("%q accepted", b)
		}
	}
}

func TestProfileName(t *testing.T) {
	id := uuid.MustParse("0d5e7c1a-1234-4bcd-9ef0-112233445566")
	if got := ProfileName(id); got != "k0d5e7c1a1234" {
		t.Fatalf("got %s", got)
	}
}

func TestProfileLimitsValidate(t *testing.T) {
	ok := ProfileLimits{MaxSessions: 10, MaxStreams: 100, MaxStreamsPerSession: 10}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := ProfileLimits{MaxStreams: 5, MaxStreamsPerSession: 10}
	if bad.Validate() == nil {
		t.Fatal("max_streams_per_session > max_streams accepted")
	}
	neg := ProfileLimits{MaxSessions: -1}
	if neg.Validate() == nil {
		t.Fatal("negative accepted")
	}
}

func TestEngineValid(t *testing.T) {
	if !EngineTProxy.Valid() || !EngineTelemt.Valid() {
		t.Fatal("tproxy and telemt must both be valid engines")
	}
	for _, bad := range []Engine{"", "TELEMT", "mtproto", "tproxy "} {
		if bad.Valid() {
			t.Errorf("engine %q must not be valid", bad)
		}
	}
}

func TestTelemtLimitsValidate(t *testing.T) {
	ok := TelemtLimits{DataQuotaBytes: 1 << 30, RateLimitUpBps: 1_000_000, RateLimitDownBps: 2_000_000, MaxUniqueIPs: 3, MaxTCPConns: 64}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid limits rejected: %v", err)
	}
	if err := (TelemtLimits{}).Validate(); err != nil {
		t.Fatalf("zero limits must be valid (inherit global): %v", err)
	}
	for name, l := range map[string]TelemtLimits{
		"quota":     {DataQuotaBytes: -1},
		"up":        {RateLimitUpBps: -1},
		"down":      {RateLimitDownBps: -1},
		"ips":       {MaxUniqueIPs: -1},
		"conns":     {MaxTCPConns: -1},
		"too large": {DataQuotaBytes: MaxTelemtQuotaBytes + 1},
		// M5: both counters reach the agent as proto3 uint32, so a value above 2^32-1 wraps
		// - 4294967297 becomes 1 and "effectively unlimited" turns into "one IP".
		"ips too large":   {MaxUniqueIPs: MaxTelemtCounter + 1},
		"conns too large": {MaxTCPConns: MaxTelemtCounter + 1},
		"ips wrap":        {MaxUniqueIPs: 4294967297},
	} {
		if err := l.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	if err := (TelemtLimits{DataQuotaBytes: MaxTelemtQuotaBytes}).Validate(); err != nil {
		t.Fatalf("quota at the cap must be accepted: %v", err)
	}
	if err := (TelemtLimits{MaxUniqueIPs: MaxTelemtCounter, MaxTCPConns: MaxTelemtCounter}).Validate(); err != nil {
		t.Fatalf("counters at the cap must be accepted: %v", err)
	}
}

func TestTelemtLimitsJSONOmitsZeroFields(t *testing.T) {
	raw, err := json.Marshal(TelemtLimits{DataQuotaBytes: 5})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"data_quota_bytes":5}` {
		t.Fatalf("json %s", raw)
	}
	var back TelemtLimits
	if err := json.Unmarshal([]byte(`{"data_quota_bytes":1,"rate_limit_up_bps":2,"rate_limit_down_bps":3,"max_unique_ips":4,"max_tcp_conns":5}`), &back); err != nil {
		t.Fatal(err)
	}
	if back != (TelemtLimits{DataQuotaBytes: 1, RateLimitUpBps: 2, RateLimitDownBps: 3, MaxUniqueIPs: 4, MaxTCPConns: 5}) {
		t.Fatalf("round trip %+v", back)
	}
}
