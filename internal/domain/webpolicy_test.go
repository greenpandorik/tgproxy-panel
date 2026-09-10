package domain

import "testing"

func TestDefaultWebPolicyIsAccepted(t *testing.T) {
	if err := DefaultWebPolicy().Validate(); err != nil {
		t.Fatalf("the default we ship must be valid: %v", err)
	}
}

func TestWebPolicyRejectsWhatTelemtRejects(t *testing.T) {
	cases := map[string]func(*WebPolicy){
		"three deadlines instead of four": func(p *WebPolicy) {
			p.Timeouts.NegotiationDeadlinesSecs = []int{3, 5, 8}
		},
		"deadlines that do not increase": func(p *WebPolicy) {
			p.Timeouts.NegotiationDeadlinesSecs = []int{3, 5, 5, 12}
		},
		"probe coalesce above 10ms":   func(p *WebPolicy) { p.Timeouts.ProbeCoalesceMs = 250 },
		"learning window above a day": func(p *WebPolicy) { p.Timeouts.CarrierLearningSecs = 90000 },
		"bridge retry below request":  func(p *WebPolicy) { p.Timeouts.BridgeRetrySecs = 5 },
		"empty carrier list":          func(p *WebPolicy) { p.Carriers = []Carrier{} },
		"duplicate carrier": func(p *WebPolicy) {
			p.Carriers = []Carrier{CarrierHTTPS, CarrierHTTPS}
		},
		"carrier telemt does not know": func(p *WebPolicy) { p.Carriers = []Carrier{"quic"} },
		"unknown aggressiveness":       func(p *WebPolicy) { p.Aggressiveness = "reckless" },
		"unknown overload preset":      func(p *WebPolicy) { p.Overload.Preset = "maximum" },
		"unknown capacity action":      func(p *WebPolicy) { p.Overload.ConnectionCapacityAction = "queue" },
	}
	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			p := DefaultWebPolicy()
			break_(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("accepted a policy telemt would refuse, which takes the node down")
			}
		})
	}
}

func TestProfileLimitsMayNotExceedTheNodeCeiling(t *testing.T) {
	g := DefaultWebGlobalLimits()
	if err := (WebProfileLimits{MaxSessions: 8, MaxStreams: 512, MaxStreamsPerSession: 64}).Validate(g); err != nil {
		t.Fatalf("a limit under the ceiling is fine: %v", err)
	}
	if err := (WebProfileLimits{MaxSessions: 200}).Validate(g); err == nil {
		t.Fatal("200 sessions is above the node's 128 and telemt refuses the whole config")
	}
	if err := (WebProfileLimits{MaxStreams: 5000}).Validate(g); err == nil {
		t.Fatal("streams above the ceiling must be refused here, not on the node")
	}
}

func TestUnsetProfileLimitIsNotAViolation(t *testing.T) {
	if err := (WebProfileLimits{}).Validate(DefaultWebGlobalLimits()); err != nil {
		t.Fatalf("no limits set at all is the default case: %v", err)
	}
}
