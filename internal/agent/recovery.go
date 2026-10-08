package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/net/proxy"

	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/telemt"
	agentv1 "tgwebproxy/proto/agent/v1"
)

type recoveryState struct {
	Policy reliability.Policy  `json:"policy"`
	Active string              `json:"active"`
	Events []reliability.Event `json:"events"`
	Last   time.Time           `json:"last"`
}

func (h *Handler) recoveryPath() string { return filepath.Join(h.cfg.StateDir, "reliability.json") }
func (h *Handler) saveRecoveryLocked() error {
	b, e := json.Marshal(recoveryState{h.recoveryPolicy, h.activeEgress, h.recoveryEvents, h.recoveryLast})
	if e != nil {
		return e
	}
	if e = os.MkdirAll(h.cfg.StateDir, 0o700); e != nil {
		return e
	}
	return writeAtomic(h.recoveryPath(), b, 0o600)
}

func (h *Handler) loadRecovery() error {
	b, e := os.ReadFile(h.recoveryPath())
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	var s recoveryState
	if e = json.Unmarshal(b, &s); e != nil {
		return e
	}
	if e = s.Policy.Validate(); e != nil {
		return e
	}
	h.recoveryMu.Lock()
	defer h.recoveryMu.Unlock()
	h.recoveryPolicy = s.Policy
	h.activeEgress = s.Active
	h.recoveryEvents = s.Events
	h.recoveryLast = s.Last
	return nil
}

func (h *Handler) configureReliability(ctx context.Context, raw []byte) ([]byte, error) {
	if !h.maintenance.TryLock() {
		return nil, errors.New("node maintenance is already in progress")
	}
	defer h.maintenance.Unlock()
	if len(raw) > 0 {
		if e := h.checkUpdateJournal(); e != nil {
			return nil, e
		}
		if h.cfg.Engine != EngineTelemt {
			return nil, errors.New("recovery and egress require telemt")
		}
		var p reliability.Policy
		if e := json.Unmarshal(raw, &p); e != nil {
			return nil, e
		}
		if e := p.Validate(); e != nil {
			return nil, e
		}
		h.recoveryMu.Lock()
		old := h.recoveryPolicy
		oldActive := h.activeEgress
		h.recoveryMu.Unlock()
		restorePrimary := p.RestorePrimary
		p.RestorePrimary = false
		desired := p.Egress
		if p.Egress == "socks5" {
			desired = p.SOCKSAddress
		}
		changeRoute := p.Egress != "unmanaged" && (p.Egress != old.Egress || p.SOCKSAddress != old.SOCKSAddress || (restorePrimary && oldActive != desired))
		if p.Egress == "unmanaged" && old.Egress != "unmanaged" && oldActive != "direct" {
			return nil, errors.New("restore the direct route before handing routing back to manual control")
		}
		var previousUpstreams any
		if changeRoute {
			cfg, _, err := h.tm.GetConfig(ctx)
			if err != nil {
				return nil, err
			}
			previousUpstreams = cfg["upstreams"]
			if p.Egress == "unmanaged" {
				return nil, errors.New("restore the direct route before handing routing back to manual control")
			}
			addr := ""
			if p.Egress == "socks5" {
				addr = p.SOCKSAddress
			}
			if e := h.applyEgress(ctx, addr); e != nil {
				return nil, e
			}
		}
		h.recoveryMu.Lock()
		previous := h.recoveryPolicy
		h.recoveryPolicy = p
		if changeRoute || p.Egress == "unmanaged" {
			h.activeEgress = p.Egress
			if p.Egress == "socks5" {
				h.activeEgress = p.SOCKSAddress
			}
		}
		e := h.saveRecoveryLocked()
		if e != nil {
			h.recoveryPolicy = previous
			h.activeEgress = oldActive
		}
		h.recoveryMu.Unlock()
		if e != nil {
			if changeRoute {
				rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
				defer cancel()
				if previousUpstreams == nil {
					previousUpstreams = []any{}
				}
				_, restore := h.tm.PatchConfig(rollback, map[string]any{"upstreams": previousUpstreams}, true)
				if restore == nil {
					restore = h.restartTelemt(rollback)
				}
				e = errors.Join(e, restore)
			}
			return nil, e
		}
	}
	h.recoveryMu.Lock()
	defer h.recoveryMu.Unlock()
	return json.Marshal(recoveryState{h.recoveryPolicy, h.activeEgress, h.recoveryEvents, h.recoveryLast})
}

// Routes only the engine's unscoped outbound traffic. Host networking and the agent remain untouched.
func (h *Handler) applyEgress(ctx context.Context, addr string) error {
	cfg, _, e := h.tm.GetConfig(ctx)
	if e != nil {
		return e
	}
	general, _ := cfg["general"].(map[string]any)
	if middle, _ := general["use_middle_proxy"].(bool); middle && addr != "" {
		return errors.New("SOCKS/WARP egress conflicts with Middle Proxy; disable the sponsor tag first")
	}
	if e = probeEgress(ctx, addr); e != nil {
		return fmt.Errorf("candidate route cannot reach Telegram: %w", e)
	}
	old := cfg["upstreams"]
	next := []any{}
	if items, ok := old.([]any); ok {
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if hasScope(m["scopes"]) {
				next = append(next, item)
			}
		}
	}
	route := map[string]any{"type": "direct", "weight": 1, "enabled": true}
	if addr != "" {
		route["type"] = "socks5"
		route["address"] = addr
	}
	next = append(next, route)
	out, e := h.tm.PatchConfig(ctx, map[string]any{"upstreams": next}, true)
	if e == nil && (out.RestartRequired || out.ProcessRestartRequired) {
		e = h.restartTelemt(ctx)
	}
	if e == nil {
		e = h.waitTelemtReady(ctx)
	}
	if e == nil {
		return nil
	}
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	if old == nil {
		old = []any{}
	}
	_, restore := h.tm.PatchConfig(recoveryCtx, map[string]any{"upstreams": old}, true)
	if restore == nil {
		restore = h.restartTelemt(recoveryCtx)
	}
	return errors.Join(e, restore)
}

func probeEgress(ctx context.Context, addr string) error {
	var dial proxy.ContextDialer = &net.Dialer{Timeout: 4 * time.Second}
	if addr != "" {
		d, e := proxy.SOCKS5("tcp", addr, nil, &net.Dialer{Timeout: 4 * time.Second})
		if e != nil {
			return e
		}
		var ok bool
		dial, ok = d.(proxy.ContextDialer)
		if !ok {
			return errors.New("SOCKS dialer lacks deadlines")
		}
	}
	// Distinct Telegram DCs; this is reachability, not an authenticated MTProto test.
	for _, target := range []string{"149.154.175.50:443", "149.154.167.51:443"} {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		c, e := dial.DialContext(cctx, "tcp", target)
		cancel()
		if e != nil {
			return e
		}
		_ = c.Close()
	}
	return nil
}

func (h *Handler) recoveryEvent(action, result string) {
	h.recoveryMu.Lock()
	defer h.recoveryMu.Unlock()
	h.recoveryEvents = append(h.recoveryEvents, reliability.Event{At: time.Now().UTC(), Action: action, Result: result})
	if len(h.recoveryEvents) > 50 {
		h.recoveryEvents = h.recoveryEvents[len(h.recoveryEvents)-50:]
	}
	if e := h.saveRecoveryLocked(); e != nil {
		h.log.Error("persist recovery state", "err", e)
	}
}

func (h *Handler) recoveryTick(ctx context.Context) {
	if h.tm == nil || !h.maintenance.TryLock() {
		return
	}
	defer h.maintenance.Unlock()
	h.recoveryMu.Lock()
	p := h.recoveryPolicy
	active := h.activeEgress
	h.recoveryMu.Unlock()
	if p.Maintenance || h.checkUpdateJournal() != nil {
		return
	}
	// Never restart a deliberately stopped service or undo an operator's WEB pause.
	if !h.unitActive(ctx, "telemt") {
		return
	}
	web, e := h.tm.WebStatus(ctx)
	if e == nil && web.OperatorLifecycle != nil && !web.OperatorLifecycle.AdmissionOpen {
		return
	}
	ready, re := h.tm.Ready(ctx)
	if ctx.Err() != nil || (re == nil && !ready.Ready && ready.Reason == "admission_closed") {
		return
	}
	// A valid unready response means the control API is working. In telemt it
	// describes an admission pause or unavailable upstreams, neither of which a
	// process restart repairs. Unknown future readiness reasons stay observable
	// without authorizing a restart. Route failures can still use failover below.
	badService := re != nil
	var apiErr *telemt.APIError
	if errors.As(re, &apiErr) && apiErr.Status < http.StatusInternalServerError {
		// Restarting cannot repair credentials, unsupported endpoints or rate limits.
		badService = false
	}
	routeBad := false
	if p.AutomaticFailover {
		st, e := h.tm.UpstreamsStats(ctx)
		routeBad = e == nil && st.Enabled && len(st.Upstreams) > 0
		for _, u := range st.Upstreams {
			if u.LastCheckAgeSecs > 120 {
				routeBad = false
			}
			if u.Healthy && u.LastCheckAgeSecs <= 120 {
				routeBad = false
			}
		}
	}
	h.recoveryMu.Lock()
	if !badService && !routeBad {
		h.recoveryFailures = 0
		h.recoveryMu.Unlock()
		return
	}
	h.recoveryFailures++
	count := h.recoveryFailures
	last := h.recoveryLast
	actions := 0
	for _, ev := range h.recoveryEvents {
		if ev.Action == "attempt" && time.Since(ev.At) < time.Hour {
			actions++
		}
	}
	h.recoveryMu.Unlock()
	if count < p.FailureThreshold || time.Since(last) < time.Duration(p.CooldownSeconds)*time.Second || actions >= p.MaxActionsHour {
		return
	}
	if p.Recovery != "restart" && (!routeBad || !p.AutomaticFailover) {
		return
	}
	h.recoveryMu.Lock()
	h.recoveryLast = time.Now().UTC()
	h.recoveryEvents = append(h.recoveryEvents, reliability.Event{At: h.recoveryLast, Action: "attempt", Result: "confirmed repeated failure"})
	if len(h.recoveryEvents) > 50 {
		h.recoveryEvents = h.recoveryEvents[len(h.recoveryEvents)-50:]
	}
	err := h.saveRecoveryLocked()
	h.recoveryMu.Unlock()
	if err != nil {
		return
	}
	switch {
	case routeBad && p.AutomaticFailover && active != p.ReserveSOCKSAddress:
		e = h.applyEgress(ctx, p.ReserveSOCKSAddress)
		if e == nil {
			h.recoveryMu.Lock()
			h.activeEgress = p.ReserveSOCKSAddress
			h.recoveryMu.Unlock()
		}
	case badService && p.Recovery == "restart":
		e = h.restartTelemt(ctx)
	default:
		return
	}
	result := "recovered"
	if e != nil {
		result = "failed: " + e.Error()
	}
	h.recoveryEvent("result", result)
}

func (h *Handler) runRecovery(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c, cancel := context.WithTimeout(ctx, 25*time.Second)
			h.recoveryTick(c)
			cancel()
		}
	}
}

func reliabilityReply(raw []byte) *agentv1.Response {
	return &agentv1.Response{Body: &agentv1.Response_Reliability{Reliability: &agentv1.ReliabilityResponse{Json: raw}}}
}

func hasScope(value any) bool {
	switch v := value.(type) {
	case string:
		return v != ""
	case []any:
		return len(v) > 0
	case []string:
		return len(v) > 0
	default:
		return value != nil
	}
}
