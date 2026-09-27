// Package reliability defines the versioned contract for node operations.
package reliability

import (
	"errors"
	"net"
	"strconv"
	"time"
)

type Policy struct {
	RestorePrimary      bool   `json:"restore_primary,omitempty"` // One-shot request; cleared before persistence.
	Recovery            string `json:"recovery"`                  // observe | restart
	Maintenance         bool   `json:"maintenance"`
	FailureThreshold    int    `json:"failure_threshold"`
	CooldownSeconds     int    `json:"cooldown_seconds"`
	MaxActionsHour      int    `json:"max_actions_hour"`
	Egress              string `json:"egress"` // unmanaged | direct | socks5
	SOCKSAddress        string `json:"socks_address"`
	ReserveSOCKSAddress string `json:"reserve_socks_address"`
	AutomaticFailover   bool   `json:"automatic_failover"`
}

func DefaultPolicy() Policy {
	return Policy{Recovery: "observe", FailureThreshold: 3, CooldownSeconds: 300, MaxActionsHour: 2, Egress: "unmanaged"}
}

func (p Policy) Validate() error {
	if p.Recovery != "observe" && p.Recovery != "restart" {
		return errors.New("recovery must be observe or restart")
	}
	if p.FailureThreshold < 2 || p.FailureThreshold > 20 || p.CooldownSeconds < 60 || p.CooldownSeconds > 86400 || p.MaxActionsHour < 1 || p.MaxActionsHour > 6 {
		return errors.New("invalid recovery thresholds")
	}
	if p.Egress != "unmanaged" && p.Egress != "direct" && p.Egress != "socks5" {
		return errors.New("invalid egress mode")
	}
	for _, addr := range []string{p.SOCKSAddress, p.ReserveSOCKSAddress} {
		if addr == "" {
			continue
		}
		host, port, e := net.SplitHostPort(addr)
		n, ne := strconv.Atoi(port)
		ip := net.ParseIP(host)
		if e != nil || ne != nil || n < 1 || n > 65535 || ip == nil || !ip.IsLoopback() {
			return errors.New("SOCKS endpoint must be a numeric loopback address and port; run the tunnel on this node")
		}
	}
	if p.Egress == "socks5" && p.SOCKSAddress == "" {
		return errors.New("primary SOCKS endpoint is required")
	}
	if p.AutomaticFailover && p.Egress == "socks5" && p.ReserveSOCKSAddress == p.SOCKSAddress {
		return errors.New("reserve endpoint must differ from primary")
	}
	if p.AutomaticFailover && (p.Egress == "unmanaged" || p.ReserveSOCKSAddress == "") {
		return errors.New("automatic failover requires managed egress and a reserve SOCKS endpoint")
	}
	return nil
}

type Window struct {
	Seconds float64   `json:"seconds"`
	Success int64     `json:"success"`
	Failed  int64     `json:"failed"`
	At      time.Time `json:"at"`
}
type Route struct {
	Kind    string `json:"kind"`
	Healthy bool   `json:"healthy"`
	Age     int64  `json:"age_seconds"`
	DCs     int    `json:"dcs"`
}
type Resources struct {
	FDUsed            *uint64  `json:"fd_used"`
	FDLimit           *uint64  `json:"fd_limit"`
	ConntrackUsed     *uint64  `json:"conntrack_used"`
	ConntrackLimit    *uint64  `json:"conntrack_limit"`
	InodesUsedPercent *float64 `json:"inodes_used_percent"`
	TCPRetransmits    *uint64  `json:"tcp_retransmits_total"`
	ListenDrops       *uint64  `json:"listen_drops_total"`
	OOMKills          *uint64  `json:"oom_kills_total"`
}
type Event struct {
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	Result string    `json:"result"`
}
type DC struct {
	ID       int `json:"dc"`
	Alive    int `json:"alive_writers"`
	Required int `json:"required_writers"`
}
type Report struct {
	Version             int       `json:"version"`
	At                  time.Time `json:"at"`
	Connections         *Window   `json:"connections"`
	Routes              []Route   `json:"routes"`
	Resources           Resources `json:"resources"`
	DCs                 []DC      `json:"dcs"`
	Policy              Policy    `json:"policy"`
	ActiveEgress        string    `json:"active_egress"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	Events              []Event   `json:"events"`
}
