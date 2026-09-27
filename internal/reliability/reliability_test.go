package reliability

import (
	"testing"
	"time"
)

func TestFindingsUnknownCannotResolve(t *testing.T) {
	now := time.Now()
	r := Report{Version: 1, At: now, Routes: []Route{{Healthy: true, Age: 121}}, Connections: &Window{At: now, Success: 0, Failed: 0}, DCs: []DC{{ID: -2, Required: 5, Alive: 0}}}
	f := Findings(r, now)
	if len(f) != 2 || f[0].Known || f[1].Kind != "dc_-2" || !f[1].Failed {
		t.Fatalf("findings: %+v", f)
	}
	r.At = now.Add(-3 * time.Minute)
	if len(Findings(r, now)) != 0 {
		t.Fatal("stale observation used")
	}
}

func TestPolicyRestrictsEgress(t *testing.T) {
	p := DefaultPolicy()
	if p.Validate() != nil {
		t.Fatal("default invalid")
	}
	p.Egress = "socks5"
	for _, addr := range []string{"example.org:1080", "10.0.0.1:1080", "127.0.0.1:0", "127.0.0.1:65536"} {
		p.SOCKSAddress = addr
		if p.Validate() == nil {
			t.Errorf("accepted %s", addr)
		}
	}
	p.SOCKSAddress = "[::1]:1080"
	if p.Validate() != nil {
		t.Fatal("loopback rejected")
	}
	p.AutomaticFailover = true
	if p.Validate() == nil {
		t.Fatal("accepted missing reserve")
	}
}

func TestProbeRejectsReplayAndUnknownStatus(t *testing.T) {
	now := time.Now()
	ok := ProbeCheck{Status: "not_run"}
	p := ProbeReport{At: now, Location: "isp-1", TLS: ok, HTTP: ok, FakeTLS: ok, WEB: ok}
	if p.Validate(now) != nil {
		t.Fatal("valid rejected")
	}
	p.At = now.Add(-3 * time.Minute)
	if p.Validate(now) == nil {
		t.Fatal("stale accepted")
	}
	p.At = now
	p.WEB.Status = ""
	if p.Validate(now) == nil {
		t.Fatal("unknown accepted")
	}
}
