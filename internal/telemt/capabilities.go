package telemt

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// TelemtCapabilities is what a node can do, as the panel works it out for itself:
// /v1/system/info carries no feature flags.
type TelemtCapabilities struct {
	Web                  bool
	CarrierNegotiation   bool
	CarrierLearning      bool
	CarrierLearningReset bool
	WebPause             bool
	WebDrain             bool
	WebResume            bool
	WebRuntime           bool
	HttpUpstreamDecoy    bool
	TLSEmulation         bool
	MiddleProxy          bool
}

// Capability names, matching the TelemtCapabilities field names.
const (
	CapWeb                  = "Web"
	CapCarrierNegotiation   = "CarrierNegotiation"
	CapCarrierLearning      = "CarrierLearning"
	CapCarrierLearningReset = "CarrierLearningReset"
	CapWebPause             = "WebPause"
	CapWebDrain             = "WebDrain"
	CapWebResume            = "WebResume"
	CapWebRuntime           = "WebRuntime"
	CapHTTPUpstreamDecoy    = "HttpUpstreamDecoy"
	CapTLSEmulation         = "TLSEmulation"
	CapMiddleProxy          = "MiddleProxy"
)

// UnknownCapabilities holds the capabilities the probe could not settle. A capability listed
// here is "we could not ask", which the panel renders as Not available; false means the node
// answered and does not support it.
type UnknownCapabilities map[string]struct{}

func (u UnknownCapabilities) Has(name string) bool {
	_, ok := u[name]
	return ok
}

func (u UnknownCapabilities) Any() bool { return len(u) > 0 }

// Names returns the unknown capabilities in sorted order.
func (u UnknownCapabilities) Names() []string {
	out := make([]string, 0, len(u))
	for name := range u {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// CapabilityProbe is what the computation reads. Every field is a read-only observation; no
// capability is ever established by calling an endpoint that changes state.
type CapabilityProbe struct {
	Version   string
	Status    *WebStatus
	StatusErr error
	Config    map[string]any
	ConfigErr error
}

// capabilityMinVersion is the earliest telemt release known to carry each capability: WEB mode
// and its decoy arrived in 3.5.1, the WEB runtime and its lifecycle in 3.5.6, carrier learning
// in 3.5.7.
var capabilityMinVersion = map[string][3]int{
	CapWeb:                  {3, 5, 1},
	CapCarrierNegotiation:   {3, 5, 1},
	CapHTTPUpstreamDecoy:    {3, 5, 1},
	CapWebRuntime:           {3, 5, 6},
	CapWebPause:             {3, 5, 6},
	CapWebDrain:             {3, 5, 6},
	CapWebResume:            {3, 5, 6},
	CapCarrierLearning:      {3, 5, 7},
	CapCarrierLearningReset: {3, 5, 7},
	CapTLSEmulation:         {3, 5, 0},
	CapMiddleProxy:          {3, 5, 0},
}

type tri int8

const (
	triUnknown tri = iota
	triNo
	triYes
)

// Capabilities probes the node read-only and computes what it supports.
func (c *Client) Capabilities(ctx context.Context) (TelemtCapabilities, UnknownCapabilities, error) {
	info, err := c.SystemInfo(ctx)
	if err != nil {
		return TelemtCapabilities{}, nil, err
	}
	p := CapabilityProbe{Version: info.Version}
	if st, err := c.WebStatus(ctx); err == nil {
		p.Status = &st
	} else {
		p.StatusErr = err
	}
	if cfg, _, err := c.GetConfig(ctx); err == nil {
		p.Config = cfg
	} else {
		p.ConfigErr = err
	}
	caps, unknown := ComputeCapabilities(p)
	return caps, unknown, nil
}

// ComputeCapabilities turns one probe into capabilities plus the set it could not determine.
func ComputeCapabilities(p CapabilityProbe) (TelemtCapabilities, UnknownCapabilities) {
	state := map[string]tri{}
	version, versionOK := ParseVersion(p.Version)
	for name, want := range capabilityMinVersion {
		if !versionOK {
			continue
		}
		if compareVersion(version, want) >= 0 {
			state[name] = triYes
		} else {
			state[name] = triNo
		}
	}

	webRuntimeCaps := []string{CapWebRuntime, CapWebPause, CapWebDrain, CapWebResume, CapCarrierLearning, CapCarrierLearningReset}
	switch {
	case p.Status != nil:
		state[CapWeb] = triYes
		state[CapWebRuntime] = triYes
		if p.Status.OperatorLifecycle != nil {
			state[CapWebPause], state[CapWebDrain], state[CapWebResume] = triYes, triYes, triYes
		}
		if p.Status.HasCarrierNegotiation() {
			state[CapCarrierNegotiation] = triYes
		}
		if p.Status.Runtime.Learning != nil {
			state[CapCarrierLearning] = triYes
		}
	case IsNotFound(p.StatusErr):
		for _, name := range webRuntimeCaps {
			state[name] = triNo
		}
	case endpointAnswered(p.StatusErr):
		state[CapWebRuntime] = triYes
	}

	if p.Config != nil {
		if _, ok := p.Config["web"]; ok {
			state[CapWeb] = triYes
		}
		if configHasKey(p.Config, "censorship", "tls_emulation") {
			state[CapTLSEmulation] = triYes
		}
		if configHasKey(p.Config, "general", "use_middle_proxy") {
			state[CapMiddleProxy] = triYes
		}
		if configHasHTTPUpstreamDecoy(p.Config) {
			state[CapHTTPUpstreamDecoy] = triYes
		}
	}

	unknown := UnknownCapabilities{}
	set := func(name string) bool {
		switch state[name] {
		case triYes:
			return true
		case triNo:
			return false
		default:
			unknown[name] = struct{}{}
			return false
		}
	}
	caps := TelemtCapabilities{
		Web:                  set(CapWeb),
		CarrierNegotiation:   set(CapCarrierNegotiation),
		CarrierLearning:      set(CapCarrierLearning),
		CarrierLearningReset: set(CapCarrierLearningReset),
		WebPause:             set(CapWebPause),
		WebDrain:             set(CapWebDrain),
		WebResume:            set(CapWebResume),
		WebRuntime:           set(CapWebRuntime),
		HttpUpstreamDecoy:    set(CapHTTPUpstreamDecoy),
		TLSEmulation:         set(CapTLSEmulation),
		MiddleProxy:          set(CapMiddleProxy),
	}
	return caps, unknown
}

// endpointAnswered reports whether err came from telemt itself rather than from a transport
// failure or a proxy, i.e. the endpoint exists but refused this particular call.
func endpointAnswered(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Code != "" && apiErr.Code != "unexpected_response"
}

func configHasKey(cfg map[string]any, section, key string) bool {
	sub, ok := cfg[section].(map[string]any)
	if !ok {
		return false
	}
	_, ok = sub[key]
	return ok
}

func configHasHTTPUpstreamDecoy(cfg map[string]any) bool {
	web, ok := cfg["web"].(map[string]any)
	if !ok {
		return false
	}
	vhosts, ok := web["vhosts"].([]any)
	if !ok {
		return false
	}
	for _, raw := range vhosts {
		vhost, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		decoy, ok := vhost["decoy"].(map[string]any)
		if !ok {
			continue
		}
		if _, ok := decoy["http_upstream"]; ok {
			return true
		}
		if _, ok := decoy["upstream"]; ok {
			return true
		}
		if mode, ok := decoy["mode"].(string); ok && mode == "http_upstream" {
			return true
		}
	}
	return false
}

// ParseVersion reads "3.5.7", "telemt 3.5.7" or "3.5.7-rc1" into its numeric parts.
func ParseVersion(s string) ([3]int, bool) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return [3]int{}, false
	}
	raw := fields[len(fields)-1]
	raw = strings.TrimPrefix(raw, "v")
	if i := strings.IndexAny(raw, "-+"); i >= 0 {
		raw = raw[:i]
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

func compareVersion(a, b [3]int) int {
	for i := range a {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}
