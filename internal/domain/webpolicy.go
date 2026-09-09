package domain

import (
	"errors"
	"fmt"
)

type Carrier string

const (
	CarrierHTTPS          Carrier = "https"
	CarrierHTTPSLanes     Carrier = "https-lanes"
	CarrierWebSocket      Carrier = "websocket"
	CarrierWebSocketLanes Carrier = "websocket-lanes"
)

func (c Carrier) Valid() bool {
	switch c {
	case CarrierHTTPS, CarrierHTTPSLanes, CarrierWebSocket, CarrierWebSocketLanes:
		return true
	}
	return false
}

type Aggressiveness string

const (
	AggressivenessConservative Aggressiveness = "conservative"
	AggressivenessBalanced     Aggressiveness = "balanced"
	AggressivenessAggressive   Aggressiveness = "aggressive"
)

func (a Aggressiveness) Valid() bool {
	switch a {
	case AggressivenessConservative, AggressivenessBalanced, AggressivenessAggressive:
		return true
	}
	return false
}

type WebPreset string

const (
	PresetAutomatic     WebPreset = "automatic"
	PresetCompatibility WebPreset = "compatibility"
	PresetPreferWS      WebPreset = "prefer_websocket"
	PresetHTTPSOnly     WebPreset = "https_only"
	PresetCustom        WebPreset = "custom"
)

// WebTimeouts mirrors telemt's [web.timeouts]. Every bound below is telemt's own; sending a
// value outside them is not clamped by telemt, it refuses the config.
type WebTimeouts struct {
	NegotiationDeadlinesSecs []int `json:"carrier_negotiation_deadlines_secs"`
	CarrierHealthSecs        int   `json:"carrier_health_secs"`
	CarrierLearningSecs      int   `json:"carrier_learning_secs"`
	BridgeRequestSecs        int   `json:"bridge_request_secs"`
	BridgeRetrySecs          int   `json:"bridge_retry_secs"`
	ProbeCoalesceMs          int   `json:"carrier_probe_coalesce_ms"`
}

type WebPolicy struct {
	Preset          WebPreset      `json:"preset"`
	Carrier         Carrier        `json:"carrier"`
	Carriers        []Carrier      `json:"carriers"`
	CarrierLearning bool           `json:"carrier_learning"`
	Aggressiveness  Aggressiveness `json:"carrier_negotiation_aggressiveness"`
	Timeouts        WebTimeouts    `json:"timeouts"`
}

func DefaultWebPolicy() WebPolicy {
	return WebPolicy{
		Preset:          PresetAutomatic,
		Carrier:         CarrierHTTPS,
		Carriers:        []Carrier{CarrierWebSocketLanes, CarrierWebSocket, CarrierHTTPSLanes},
		CarrierLearning: true,
		Aggressiveness:  AggressivenessConservative,
		Timeouts: WebTimeouts{
			NegotiationDeadlinesSecs: []int{3, 5, 8, 12},
			CarrierHealthSecs:        30,
			CarrierLearningSecs:      600,
			BridgeRequestSecs:        10,
			BridgeRetrySecs:          90,
			ProbeCoalesceMs:          0,
		},
	}
}

// negotiationDeadlines is fixed at four by telemt, not a range.
const negotiationDeadlines = 4

func (t WebTimeouts) Validate() error {
	if len(t.NegotiationDeadlinesSecs) != negotiationDeadlines {
		return fmt.Errorf("carrier_negotiation_deadlines_secs: %d values, telemt takes exactly %d", len(t.NegotiationDeadlinesSecs), negotiationDeadlines)
	}
	for i, d := range t.NegotiationDeadlinesSecs {
		if d < 1 {
			return errors.New("carrier_negotiation_deadlines_secs: values must be positive")
		}
		if i > 0 && d <= t.NegotiationDeadlinesSecs[i-1] {
			return errors.New("carrier_negotiation_deadlines_secs: values must strictly increase")
		}
	}
	if t.CarrierHealthSecs < 1 {
		return errors.New("carrier_health_secs: must be positive")
	}
	if t.CarrierLearningSecs < 2 || t.CarrierLearningSecs > 86400 {
		return fmt.Errorf("carrier_learning_secs: %d out of 2..86400", t.CarrierLearningSecs)
	}
	if t.BridgeRequestSecs < 1 || t.BridgeRequestSecs > 60 {
		return fmt.Errorf("bridge_request_secs: %d out of 1..60", t.BridgeRequestSecs)
	}
	if t.BridgeRetrySecs < 1 || t.BridgeRetrySecs > 300 {
		return fmt.Errorf("bridge_retry_secs: %d out of 1..300", t.BridgeRetrySecs)
	}
	if t.BridgeRetrySecs < t.BridgeRequestSecs {
		return errors.New("bridge_retry_secs: must not be below bridge_request_secs")
	}
	if t.ProbeCoalesceMs < 0 || t.ProbeCoalesceMs > 10 {
		return fmt.Errorf("carrier_probe_coalesce_ms: %d out of 0..10", t.ProbeCoalesceMs)
	}
	return nil
}

func (p WebPolicy) Validate() error {
	if !p.Carrier.Valid() {
		return fmt.Errorf("carrier %q is not one of https, https-lanes, websocket, websocket-lanes", p.Carrier)
	}
	if !p.Aggressiveness.Valid() {
		return fmt.Errorf("carrier_negotiation_aggressiveness %q is not one of conservative, balanced, aggressive", p.Aggressiveness)
	}
	if len(p.Carriers) == 0 {
		return errors.New("carriers: must not be empty; disable negotiation instead of sending an empty list")
	}
	seen := map[Carrier]bool{}
	for _, c := range p.Carriers {
		if !c.Valid() {
			return fmt.Errorf("carriers: %q is not a carrier telemt knows", c)
		}
		if seen[c] {
			return fmt.Errorf("carriers: %q listed twice", c)
		}
		seen[c] = true
	}
	return p.Timeouts.Validate()
}

// WebGlobalLimits are the node-wide ceilings from telemt's [web.limits]. They only change
// with a restart, which is why a per-profile limit cannot be raised past them in the same
// apply that raises them.
type WebGlobalLimits struct {
	MaxSessions          int `json:"max_sessions_global"`
	MaxStreams           int `json:"max_streams_global"`
	MaxStreamsPerSession int `json:"max_streams_per_session"`
}

func DefaultWebGlobalLimits() WebGlobalLimits {
	return WebGlobalLimits{MaxSessions: 128, MaxStreams: 4096, MaxStreamsPerSession: 128}
}

// WebProfileLimits are telemt's three per-profile WEB limits. There is deliberately no
// max_pending_per_session: telemt has no such key, and sending one fails config validation
// and leaves the node down. Zero means the limit is not set.
type WebProfileLimits struct {
	MaxSessions          int `json:"max_sessions,omitempty"`
	MaxStreams           int `json:"max_streams,omitempty"`
	MaxStreamsPerSession int `json:"max_streams_per_session,omitempty"`
}

// Validate rejects what telemt would reject, before it reaches the node. Global limits are
// restart-owned, so this is checked against the ceilings the node is running now, not the
// ones the panel would like it to have.
func (l WebProfileLimits) Validate(g WebGlobalLimits) error {
	check := func(name string, v, ceiling int) error {
		if v < 0 {
			return fmt.Errorf("%s: must not be negative", name)
		}
		if v > 0 && ceiling > 0 && v > ceiling {
			return fmt.Errorf("%s: %d is above the node's ceiling of %d; raise the node limit first, which needs a restart", name, v, ceiling)
		}
		return nil
	}
	if err := check("max_sessions", l.MaxSessions, g.MaxSessions); err != nil {
		return err
	}
	if err := check("max_streams", l.MaxStreams, g.MaxStreams); err != nil {
		return err
	}
	return check("max_streams_per_session", l.MaxStreamsPerSession, g.MaxStreamsPerSession)
}
